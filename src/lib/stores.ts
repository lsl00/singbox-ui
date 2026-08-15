import { writable } from "svelte/store";
import type {
  GroupInfo,
  ServiceStatusInfo,
  StatusInfo,
  VersionInfo,
} from "./types";

export const connected = writable(false);
export const version = writable<VersionInfo | null>(null);
export const serviceStatus = writable<ServiceStatusInfo | null>(null);
export const status = writable<StatusInfo | null>(null);
export const groups = writable<GroupInfo[]>([]);
export const startedAt = writable<number>(0);

export const statusHistory = writable<{
  uplink: number[];
  downlink: number[];
}>({ uplink: [], downlink: [] });

const MAX_HISTORY = 60;

export function appendStatus(s: StatusInfo) {
  status.set(s);
  statusHistory.update((h) => {
    const uplink = [...h.uplink, s.uplink];
    const downlink = [...h.downlink, s.downlink];
    if (uplink.length > MAX_HISTORY) uplink.shift();
    if (downlink.length > MAX_HISTORY) downlink.shift();
    return { uplink, downlink };
  });
}

export function resetAll() {
  connected.set(false);
  version.set(null);
  serviceStatus.set(null);
  status.set(null);
  groups.set([]);
  startedAt.set(0);
  statusHistory.set({ uplink: [], downlink: [] });
}
