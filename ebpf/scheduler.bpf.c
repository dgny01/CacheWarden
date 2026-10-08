#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
char LICENSE[] SEC("license") = "GPL";
struct scheduler_stats { __u64 observed_delays, total_delay_ns, max_delay_ns, schedule_ins; };
struct { __uint(type, BPF_MAP_TYPE_HASH); __uint(max_entries, 4096); __type(key, __u32); __type(value, __u8); } target_tids SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_HASH); __uint(max_entries, 16384); __type(key, __u32); __type(value, __u64); } runnable_since SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_ARRAY); __uint(max_entries, 1); __type(key, __u32); __type(value, struct scheduler_stats); } stats SEC(".maps");
/* Log2 nanosecond buckets. Bucket 63 also contains values >= 2^63 ns. */
struct { __uint(type, BPF_MAP_TYPE_ARRAY); __uint(max_entries, 64); __type(key, __u32); __type(value, __u64); } delay_histogram SEC(".maps");

static __always_inline int record_wakeup(__u32 tid)
{
	__u64 now;
	if (!bpf_map_lookup_elem(&target_tids, &tid))
		return 0;
	/* Repeated wakeups must not move the start of an existing runnable wait. */
	now = bpf_ktime_get_ns();
	bpf_map_update_elem(&runnable_since, &tid, &now, BPF_NOEXIST);
	return 0;
}
SEC("tracepoint/sched/sched_wakeup")
int handle_sched_wakeup(struct trace_event_raw_sched_wakeup_template *ctx)
{
	return record_wakeup((__u32)ctx->pid);
}
SEC("tracepoint/sched/sched_wakeup_new")
int handle_sched_wakeup_new(struct trace_event_raw_sched_wakeup_template *ctx)
{
	return record_wakeup((__u32)ctx->pid);
}
SEC("tracepoint/sched/sched_switch")
int handle_sched_switch(struct trace_event_raw_sched_switch *ctx)
{
	__u32 tid = (__u32)ctx->next_pid, zero = 0, bucket;
	__u64 *started, delay, *count;
	struct scheduler_stats *summary;
	if (!bpf_map_lookup_elem(&target_tids, &tid))
		return 0;
	summary = bpf_map_lookup_elem(&stats, &zero);
	if (summary)
		__sync_fetch_and_add(&summary->schedule_ins, 1);
	started = bpf_map_lookup_elem(&runnable_since, &tid);
	if (!started)
		return 0;
	delay = bpf_ktime_get_ns() - *started;
	bpf_map_delete_elem(&runnable_since, &tid);
	if (summary) {
		__sync_fetch_and_add(&summary->observed_delays, 1);
		__sync_fetch_and_add(&summary->total_delay_ns, delay);
		if (delay > summary->max_delay_ns)
			__sync_val_compare_and_swap(&summary->max_delay_ns, summary->max_delay_ns, delay);
	}
	bucket = delay ? 63 - __builtin_clzll(delay) : 0;
	count = bpf_map_lookup_elem(&delay_histogram, &bucket);
	if (count)
		__sync_fetch_and_add(count, 1);
	return 0;
}
