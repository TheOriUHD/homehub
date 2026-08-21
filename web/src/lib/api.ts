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
export type DnsOverview = ApiSchemas["DnsOverview"];
export type DnsTopItem = ApiSchemas["DnsTopItem"];
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
export type SSHHost = ApiSchemas["SSHHost"];
export type SSHHostInput = ApiSchemas["SSHHostInput"];
export type SSHCredentialInfo = ApiSchemas["SSHCredentialInfo"];
export type MetricQueryResult = ApiSchemas["MetricQueryResult"];
export type MetricSeriesInfo = ApiSchemas["MetricSeriesInfo"];
export type AlertRule = ApiSchemas["AlertRule"];
export type AlertRuleInput = ApiSchemas["AlertRuleInput"];
export type Channel = ApiSchemas["Channel"];
export type ChannelInput = ApiSchemas["ChannelInput"];
export type ChannelRef = ApiSchemas["ChannelRef"];
export type NotificationPrefs = ApiSchemas["NotificationPrefs"];
export type DbInstance = ApiSchemas["DbInstance"];
export type DbStats = ApiSchemas["DbStats"];
export type DbDatabase = ApiSchemas["DbDatabase"];
export type DbObject = ApiSchemas["DbObject"];
export type DbObjectSchema = ApiSchemas["DbObjectSchema"];
export type DbColumn = ApiSchemas["DbColumn"];
export type DbIndex = ApiSchemas["DbIndex"];
export type DbDataPage = ApiSchemas["DbDataPage"];
export type DbSession = ApiSchemas["DbSession"];
export type DbQueryResult = ApiSchemas["DbQueryResult"];
export type McOverview = ApiSchemas["McOverview"];
export type McServer = ApiSchemas["McServer"];
export type McStatus = ApiSchemas["McStatus"];
export type McPlayer = ApiSchemas["McPlayer"];
export type McPairingCode = ApiSchemas["McPairingCode"];
export type McConsole = ApiSchemas["McConsole"];
export type McConsoleLine = ApiSchemas["McConsoleLine"];
export type McDownload = ApiSchemas["McDownload"];
export type HaInstance = ApiSchemas["HaInstance"];
export type HaInfo = ApiSchemas["HaInfo"];
export type HaArea = ApiSchemas["HaArea"];
export type HaEntity = ApiSchemas["HaEntity"];
export type HaServiceCall = ApiSchemas["HaServiceCall"];
export type HaDashboard = ApiSchemas["HaDashboard"];
export type HaDashboardCard = ApiSchemas["HaDashboardCard"];
export type BackupTarget = ApiSchemas["BackupTarget"];
export type BackupTargetInput = ApiSchemas["BackupTargetInput"];
export type BackupArchive = ApiSchemas["BackupArchive"];
export type BackupManifest = ApiSchemas["BackupManifest"];
export type BackupRun = ApiSchemas["BackupRun"];
export type BackupSchedule = ApiSchemas["BackupSchedule"];
export type TruenasInstance = ApiSchemas["TruenasInstance"];
export type TruenasSystemInfo = ApiSchemas["TruenasSystemInfo"];
export type TruenasOverview = ApiSchemas["TruenasOverview"];
export type TruenasMemory = ApiSchemas["TruenasMemory"];
export type TruenasPool = ApiSchemas["TruenasPool"];
export type TruenasDisk = ApiSchemas["TruenasDisk"];
export type TruenasDataset = ApiSchemas["TruenasDataset"];
export type TruenasShare = ApiSchemas["TruenasShare"];
export type TruenasSnapshot = ApiSchemas["TruenasSnapshot"];
export type TruenasAlert = ApiSchemas["TruenasAlert"];
export type MoonrakerSnapshot = ApiSchemas["MoonrakerSnapshot"];
export type MoonrakerHeater = ApiSchemas["MoonrakerHeater"];
export type MoonrakerSensor = ApiSchemas["MoonrakerSensor"];
export type MoonrakerTempFan = ApiSchemas["MoonrakerTempFan"];
export type MoonrakerFan = ApiSchemas["MoonrakerFan"];
export type MoonrakerLed = ApiSchemas["MoonrakerLed"];
export type MoonrakerOutputPin = ApiSchemas["MoonrakerOutputPin"];
export type MoonrakerMacro = ApiSchemas["MoonrakerMacro"];
export type MoonrakerCamera = ApiSchemas["MoonrakerCamera"];
export type MoonrakerPrintJob = ApiSchemas["MoonrakerPrintJob"];
export type MoonrakerToolhead = ApiSchemas["MoonrakerToolhead"];
export type PatchpilotHost = ApiSchemas["PatchpilotHost"];
export type Signal = ApiSchemas["Signal"];
export type SignalCounts = ApiSchemas["SignalCounts"];
export type PatchpilotDetectResult = ApiSchemas["PatchpilotDetectResult"];
export type PatchpilotDryRunResult = ApiSchemas["PatchpilotDryRunResult"];
export type PatchpilotSchedule = ApiSchemas["PatchpilotSchedule"];
export type PatchpilotScheduleInput = ApiSchemas["PatchpilotScheduleInput"];
export type PatchpilotRun = ApiSchemas["PatchpilotRun"];
export type PatchpilotHostRun = ApiSchemas["PatchpilotHostRun"];
export type PatchpilotRunDetail = ApiSchemas["PatchpilotRunDetail"];

export type TruenasStats = ApiSchemas["TruenasStats"];
export type TruenasStatPoint = ApiSchemas["TruenasStatPoint"];
export type TruenasLoadPoint = ApiSchemas["TruenasLoadPoint"];
export type TruenasTrafficPoint = ApiSchemas["TruenasTrafficPoint"];
export type TruenasInterfaceTraffic = ApiSchemas["TruenasInterfaceTraffic"];

export type ApertureOverview = ApiSchemas["ApertureOverview"];
export type ApertureCamera = ApiSchemas["ApertureCamera"];
export type ApertureStreamGrant = ApiSchemas["ApertureStreamGrant"];

export type CaddySitesState = ApiSchemas["CaddySitesState"];
export type CaddySite = ApiSchemas["CaddySite"];
export type CaddySiteInput = ApiSchemas["CaddySiteInput"];
export type CaddyRawSite = ApiSchemas["CaddyRawSite"];
export type CaddyHeaderUp = ApiSchemas["CaddyHeaderUp"];
export type CaddyServed = ApiSchemas["CaddyServed"];
export type CaddyDnsState = ApiSchemas["CaddyDnsState"];
export type CaddyDnsRecord = ApiSchemas["CaddyDnsRecord"];
export type CaddyDnsRecordInput = ApiSchemas["CaddyDnsRecordInput"];
export type CaddyCfLinkResult = ApiSchemas["CaddyCfLinkResult"];
export type CaddyPublishResult = ApiSchemas["CaddyPublishResult"];

export type TsTailnet = ApiSchemas["TsTailnet"];
export type TsDevice = ApiSchemas["TsDevice"];
export type TsKey = ApiSchemas["TsKey"];
export type TsCreatedKey = ApiSchemas["TsCreatedKey"];
export type TsDns = ApiSchemas["TsDns"];

export type AmpPanel = ApiSchemas["AmpPanel"];
export type AmpOverview = ApiSchemas["AmpOverview"];
export type AmpTarget = ApiSchemas["AmpTarget"];
export type AmpServerInstance = ApiSchemas["AmpServerInstance"];
export type AmpInstanceStatus = ApiSchemas["AmpInstanceStatus"];
export type AmpConsoleLine = ApiSchemas["AmpConsoleLine"];

export type ConvertCatalog = ApiSchemas["ConvertCatalog"];
export type ConvertFormat = ApiSchemas["ConvertFormat"];
export type ConvertOutput = ApiSchemas["ConvertOutput"];
export type ConvertOption = ApiSchemas["ConvertOption"];
export type ConvertBatch = ApiSchemas["ConvertBatch"];
export type ConvertItem = ApiSchemas["ConvertItem"];

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
