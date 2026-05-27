import { ref } from "vue";

import { useDiscovery } from "./useApi";
import type { ScanJob } from "@/types";

const POLL_MS = 1000;
const MAX_POLLS = 240; // safety cap (~4 min)

/**
 * Runs a scan as a background job and polls until it finishes.
 * Exposes reactive `running` + `job` so callers can show live status.
 */
export function useScan() {
  const discovery = useDiscovery();
  const running = ref(false);
  const job = ref<ScanJob | null>(null);

  async function run(
    subnetId: number,
    method: "arp" | "ping" | "nmap",
    timeout: number,
  ): Promise<ScanJob> {
    running.value = true;
    try {
      const submitted = await discovery.scanAsync(subnetId, method, timeout);
      job.value = submitted;
      let polls = 0;
      while (
        job.value &&
        (job.value.status === "queued" || job.value.status === "running") &&
        polls < MAX_POLLS
      ) {
        await new Promise((r) => setTimeout(r, POLL_MS));
        job.value = await discovery.job(submitted.id);
        polls += 1;
      }
      return job.value!;
    } finally {
      running.value = false;
    }
  }

  return { running, job, run };
}
