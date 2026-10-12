#include "vmlinux.h"
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_helpers.h>
char LICENSE[] SEC("license") = "GPL";
struct scheduler_stats { __u64 observed_delays, total_delay_ns, max_delay_ns, schedule_ins; };
#define MAX_DELAY_UPDATE_RETRIES 4
struct { __uint(type, BPF_MAP_TYPE_HASH); __uint(max_entries, 4096); __type(key, __u32); __type(value, __u8); } target_tids SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_HASH); __uint(max_entries, 16384); __type(key, __u32); __type(value, __u64); } runnable_since SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_ARRAY); __uint(max_entries, 1); __type(key, __u32); __type(value, struct scheduler_stats); } stats SEC(".maps");
/* Log2 nanosecond buckets. Bucket 63 also contains values >= 2^63 ns. */
struct { __uint(type, BPF_MAP_TYPE_ARRAY); __uint(max_entries, 64); __type(key, __u32); __type(value, __u64); } delay_histogram SEC(".maps");

static __always_inline __u32 log2_floor(__u64 value)
{
	__u32 result = 0;
	if (value >> 32) { value >>= 32; result += 32; }
	if (value >> 16) { value >>= 16; result += 16; }
	if (value >> 8) { value >>= 8; result += 8; }
	if (value >> 4) { value >>= 4; result += 4; }
	if (value >> 2) { value >>= 2; result += 2; }
	if (value >> 1) result += 1;
	return result;
}

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

static __always_inline void mark_runnable(__u32 tid)
{
	__u64 now;

	if (!bpf_map_lookup_elem(&target_tids, &tid))
		return;
	now = bpf_ktime_get_ns();
	/* Preserve an earlier wakeup timestamp if one is already present. */
	bpf_map_update_elem(&runnable_since, &tid, &now, BPF_NOEXIST);
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
SEC("raw_tracepoint/sched_switch")
int handle_sched_switch(struct bpf_raw_tracepoint_args *ctx)
{
	bool preempt = (bool)ctx->args[0];
	struct task_struct *previous = (struct task_struct *)ctx->args[1];
	struct task_struct *next = (struct task_struct *)ctx->args[2];
	__u32 tid = BPF_CORE_READ(next, pid), zero = 0, bucket;
	unsigned int previous_state = BPF_CORE_READ(previous, __state);
	__u64 *started, delay, *count, old_max;
	struct scheduler_stats *summary;

	/* A task switched out while still runnable was preempted, not put to sleep. */
	if (preempt || previous_state == 0)
		mark_runnable(BPF_CORE_READ(previous, pid));
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
		old_max = summary->max_delay_ns;
		/*
		 * Keep the retry count statically bounded so the verifier can prove
		 * termination. The CAS prevents a smaller delay from overwriting a
		 * concurrent larger one. Under sustained contention all retries can
		 * lose, in which case max_delay_ns may temporarily under-report the
		 * true maximum; later observations can still raise it.
		 */
#pragma unroll
		for (int retry = 0; retry < MAX_DELAY_UPDATE_RETRIES; retry++) {
			if (delay <= old_max)
				break;
			__u64 seen = __sync_val_compare_and_swap(&summary->max_delay_ns,
							 old_max, delay);
			if (seen == old_max)
				break;
			old_max = seen;
		}
	}
	bucket = log2_floor(delay);
	count = bpf_map_lookup_elem(&delay_histogram, &bucket);
	if (count)
		__sync_fetch_and_add(count, 1);
	return 0;
}
