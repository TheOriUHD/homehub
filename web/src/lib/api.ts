import createClient from "openapi-fetch";
import type { components, paths } from "./schema";

export type ApiSchemas = components["schemas"];
export type User = ApiSchemas["User"];
export type UserDetail = ApiSchemas["UserDetail"];
export type Role = ApiSchemas["Role"];
export type Grant = ApiSchemas["Grant"];
export type GrantInput = ApiSchemas["GrantInput"];
export type Me = ApiSchemas["Me"];
export type SessionInfo = ApiSchemas["SessionInfo"];
export type Passkey = ApiSchemas["Passkey"];
export type AuditEntry = ApiSchemas["AuditEntry"];
export type AppNotification = ApiSchemas["Notification"];
export type HealthReport = ApiSchemas["HealthReport"];
export type AppSettings = ApiSchemas["AppSettings"];
export type LoginResult = ApiSchemas["LoginResult"];
export type CreateUserResult = ApiSchemas["CreateUserResult"];
export type ApiError = ApiSchemas["Error"];
export type Integration = ApiSchemas["Integration"];
export type IntegrationType = ApiSchemas["IntegrationType"];
export type TestConnectionResult = ApiSchemas["TestConnectionResult"];
export type PveSummary = ApiSchemas["PveSummary"];
export type PveNode = ApiSchemas["PveNode"];
export type PveGuest = ApiSchemas["PveGuest"];
export type PveGuestDetail = ApiSchemas["PveGuestDetail"];
export type PveGuestAction = ApiSchemas["PveGuestAction"];
export type PveSnapshot = ApiSchemas["PveSnapshot"];
export type PveTask = ApiSchemas["PveTask"];
export type PveTaskDetail = ApiSchemas["PveTaskDetail"];
export type PveStorage = ApiSchemas["PveStorage"];
export type PveBackupJob = ApiSchemas["PveBackupJob"];
export type DockerContainer = ApiSchemas["DockerContainer"];
export type DockerContainerDetail = ApiSchemas["DockerContainerDetail"];
export type DockerImage = ApiSchemas["DockerImage"];
export type DockerVolume = ApiSchemas["DockerVolume"];
export type DockerNetwork = ApiSchemas["DockerNetwork"];
export type DockerStack = ApiSchemas["DockerStack"];
export type DockerPruneResult = ApiSchemas["DockerPruneResult"];
export type DockerUpdateCheck = ApiSchemas["DockerUpdateCheck"];
export type DockerRegistryCred = ApiSchemas["DockerRegistryCred"];
export type DnsInstance = ApiSchemas["DnsInstance"];
export type DnsStats = ApiSchemas["DnsStats"];
export type DnsBlocking = ApiSchemas["DnsBlocking"];
export type DnsQuery = ApiSchemas["DnsQuery"];
export type DnsListEntry = ApiSchemas["DnsListEntry"];
export type DnsListInput = ApiSchemas["DnsListInput"];
export type DnsListType = ApiSchemas["DnsListType"];
export type DnsClient = ApiSchemas["DnsClient"];
export type DnsGroup = ApiSchemas["DnsGroup"];
export type DnsCapabilities = ApiSchemas["DnsCapabilities"];
export type DnsCompareResult = ApiSchemas["DnsCompareResult"];
export type DnsSyncResult = ApiSchemas["DnsSyncResult"];
export type TermSession = ApiSchemas["TermSession"];
export type SSHCredentialInfo = ApiSchemas["SSHCredentialInfo"];
export type MetricQueryResult = ApiSchemas["MetricQueryResult"];

export const api = createClient<paths>({ baseUrl: "/api" });

/**
 * Normalizes an openapi-fetch result into data-or-thrown-ApiFailure, so
 * callers and react-query see one error shape.
 */
export class ApiFailure extends Error {
  code: string;
  status: number;

  constructor(status: number, body: ApiError | undefined) {
    super(body?.message ?? `request failed (${status})`);
    this.code = body?.code ?? "unknown";
    this.status = status;
  }
}

export function unwrap<T>(result: {
  data?: T;
  error?: ApiError;
  response: Response;
}): T {
  if (result.error !== undefined || !result.response.ok) {
    throw new ApiFailure(result.response.status, result.error);
  }
  return result.data as T;
}

export function isApiError(err: unknown, code?: string): err is ApiFailure {
  if (!(err instanceof ApiFailure)) return false;
  return code === undefined || err.code === code;
}
