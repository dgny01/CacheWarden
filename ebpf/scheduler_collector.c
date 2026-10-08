#define _GNU_SOURCE
#include <bpf/bpf.h>
#include <bpf/libbpf.h>
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

struct scheduler_stats { uint64_t observed_delays, total_delay_ns, max_delay_ns, schedule_ins; };
static volatile sig_atomic_t stopping;
static void stop(int signal_number) { (void)signal_number; stopping = 1; }

static int add_threads(int map_fd, long process_id)
{
	char path[64];
	struct dirent *entry;
	DIR *directory;
	uint8_t marker = 1;
	snprintf(path, sizeof(path), "/proc/%ld/task", process_id);
	directory = opendir(path);
	if (!directory)
		return -errno;
	while ((entry = readdir(directory))) {
		char *end = NULL;
		errno = 0;
		unsigned long value = strtoul(entry->d_name, &end, 10);
		if (errno || end == entry->d_name || *end != '\0' || value > UINT32_MAX)
			continue;
		uint32_t tid = (uint32_t)value;
		if (bpf_map_update_elem(map_fd, &tid, &marker, BPF_ANY)) {
			int saved = errno;
			closedir(directory);
			return -saved;
		}
	}
	closedir(directory);
	return 0;
}

static uint64_t bucket_upper_bound(unsigned int bucket)
{
	return bucket >= 63 ? UINT64_MAX : (UINT64_C(1) << (bucket + 1)) - 1;
}

static uint64_t percentile_bound(const uint64_t histogram[64], uint64_t total,
				 unsigned int percentile)
{
	uint64_t rank = (total * percentile + 99) / 100, cumulative = 0;
	if (!total)
		return 0;
	for (unsigned int bucket = 0; bucket < 64; bucket++) {
		cumulative += histogram[bucket];
		if (cumulative >= rank)
			return bucket_upper_bound(bucket);
	}
	return UINT64_MAX;
}

static int write_results(const char *path, const struct scheduler_stats *stats,
			 const uint64_t histogram[64])
{
	int fd = open(path, O_WRONLY | O_CREAT | O_EXCL, 0644);
	if (fd < 0)
		return -errno;
	FILE *output = fdopen(fd, "w");
	if (!output) {
		int saved = errno;
		close(fd);
		return -saved;
	}
	double mean = stats->observed_delays
		? (double)stats->total_delay_ns / (double)stats->observed_delays : 0.0;
	fprintf(output, "metric,value,unit\n");
	fprintf(output, "observed_delays,%llu,count\n", (unsigned long long)stats->observed_delays);
	fprintf(output, "schedule_ins,%llu,count\n", (unsigned long long)stats->schedule_ins);
	fprintf(output, "total_delay_ns,%llu,ns\n", (unsigned long long)stats->total_delay_ns);
	fprintf(output, "mean_delay_ns,%.0f,ns\n", mean);
	for (unsigned int percentile = 50; percentile <= 99; percentile += percentile == 50 ? 45 : 4)
		fprintf(output, "p%u_delay_upper_bound_ns,%llu,ns\n", percentile,
			(unsigned long long)percentile_bound(histogram, stats->observed_delays, percentile));
	fprintf(output, "max_delay_ns,%llu,ns\n", (unsigned long long)stats->max_delay_ns);
	for (unsigned int bucket = 0; bucket < 64; bucket++)
		if (histogram[bucket])
			fprintf(output, "histogram_log2_ns_bucket_%u,%llu,count\n", bucket,
				(unsigned long long)histogram[bucket]);
	if (fclose(output))
		return -errno;
	return 0;
}

int main(int argc, char **argv)
{
	struct bpf_object *object = NULL;
	struct bpf_program *program;
	struct bpf_link **links = NULL;
	struct scheduler_stats stats = {};
	uint64_t histogram[64] = {};
	const char *object_path = NULL, *output_path = NULL;
	long process_id = 0;
	size_t link_count = 0, attached = 0;
	int target_fd, stats_fd, histogram_fd, result = 1;
	struct timespec interval = {.tv_nsec = 100000000};
	uint32_t zero = 0;

	for (int argument = 1; argument < argc; argument++) {
		if (!strcmp(argv[argument], "--object") && ++argument < argc)
			object_path = argv[argument];
		else if (!strcmp(argv[argument], "--pid") && ++argument < argc)
			process_id = strtol(argv[argument], NULL, 10);
		else if (!strcmp(argv[argument], "--output") && ++argument < argc)
			output_path = argv[argument];
		else {
			fprintf(stderr, "Usage: %s --object FILE --pid PID --output FILE\n", argv[0]);
			return 2;
		}
	}
	if (!object_path || !output_path || process_id <= 0) {
		fprintf(stderr, "scheduler-collector: --object, --pid, and --output are required\n");
		return 2;
	}
	libbpf_set_strict_mode(LIBBPF_STRICT_ALL);
	object = bpf_object__open_file(object_path, NULL);
	if (libbpf_get_error(object)) {
		fprintf(stderr, "scheduler-collector: cannot open BPF object %s\n", object_path);
		object = NULL;
		goto cleanup;
	}
	if (bpf_object__load(object)) {
		fprintf(stderr, "scheduler-collector: cannot load BPF programs; run with sufficient BPF/perf privileges and check kernel lockdown settings\n");
		goto cleanup;
	}
	bpf_object__for_each_program(program, object) link_count++;
	links = calloc(link_count, sizeof(*links));
	if (!links)
		goto cleanup;
	bpf_object__for_each_program(program, object) {
		links[attached] = bpf_program__attach(program);
		if (libbpf_get_error(links[attached])) {
			links[attached] = NULL;
			fprintf(stderr, "scheduler-collector: cannot attach %s; required sched tracepoints may be unavailable\n",
				bpf_program__name(program));
			goto cleanup;
		}
		attached++;
	}
	target_fd = bpf_object__find_map_fd_by_name(object, "target_tids");
	stats_fd = bpf_object__find_map_fd_by_name(object, "stats");
	histogram_fd = bpf_object__find_map_fd_by_name(object, "delay_histogram");
	if (target_fd < 0 || stats_fd < 0 || histogram_fd < 0) {
		fprintf(stderr, "scheduler-collector: required BPF maps were not found\n");
		goto cleanup;
	}
	if (add_threads(target_fd, process_id)) {
		fprintf(stderr, "scheduler-collector: cannot inspect /proc/%ld/task; verify the victim host PID and permissions\n", process_id);
		goto cleanup;
	}
	signal(SIGINT, stop);
	signal(SIGTERM, stop);
	fprintf(stderr, "scheduler-collector: observing victim process %ld; press Ctrl+C to stop\n", process_id);
	while (!stopping) {
		int refresh = add_threads(target_fd, process_id);
		if (refresh == -ENOENT)
			break;
		if (refresh) {
			fprintf(stderr, "scheduler-collector: failed to refresh victim thread IDs\n");
			goto cleanup;
		}
		nanosleep(&interval, NULL);
	}
	if (bpf_map_lookup_elem(stats_fd, &zero, &stats)) {
		fprintf(stderr, "scheduler-collector: cannot read scheduler statistics\n");
		goto cleanup;
	}
	for (uint32_t bucket = 0; bucket < 64; bucket++)
		if (bpf_map_lookup_elem(histogram_fd, &bucket, &histogram[bucket])) {
			fprintf(stderr, "scheduler-collector: cannot read delay histogram\n");
			goto cleanup;
		}
	int write_error = write_results(output_path, &stats, histogram);
	if (write_error) {
		fprintf(stderr, "scheduler-collector: cannot create result file %s: %s\n",
			output_path, strerror(-write_error));
		goto cleanup;
	}
	result = 0;
cleanup:
	for (size_t link = 0; link < attached; link++)
		bpf_link__destroy(links[link]);
	free(links);
	bpf_object__close(object);
	return result;
}
