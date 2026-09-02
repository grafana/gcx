package faro

import (
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/query/pinot"
)

const (
	appTypeWeb    = "web"
	appTypeMobile = "mobile"

	datasourceLoki  = "loki"
	datasourcePinot = "pinot"

	// Grafana's Loki plugin commonly defaults maxLines to 1000 and will
	// clamp higher values. Page at that size and continue while a page is
	// full so a long session is not cut off at Explore's old 2000-row cap.
	lokiEventsPageSize = 1000

	lokiKindEvent       = "event"
	lokiKindException   = "exception"
	lokiKindLog         = "log"
	lokiKindMeasurement = "measurement"
)

// Same clause the session-detail journey emits for mobile apps, on the
// measurements leg only (web omits it).
const mobileMeasurementFilter = ` AND measurementType NOT IN ('app_memory', 'app_cpu_usage')`

const pinotEventsMetadataSQL = `SET useMultistageEngine = true;
SELECT
  FIRSTWITHTIME(appName, "timestamp", 'STRING') FILTER (WHERE appName <> '' AND appName <> 'null') AS app_name,
  FIRSTWITHTIME(appVersion, "timestamp", 'STRING') FILTER (WHERE appVersion <> '' AND appVersion <> 'null') AS app_version,
  FIRSTWITHTIME(browserName, "timestamp", 'STRING') FILTER (WHERE browserName <> '' AND browserName <> 'null') AS browser_name,
  FIRSTWITHTIME(browserVersion, "timestamp", 'STRING') FILTER (WHERE browserVersion <> '' AND browserVersion <> 'null') AS browser_version,
  FIRSTWITHTIME(browserOs, "timestamp", 'STRING') FILTER (WHERE browserOs <> '' AND browserOs <> 'null') AS browser_os,
  FIRSTWITHTIME(geoCountryCode, "timestamp", 'STRING') FILTER (WHERE geoCountryCode <> '' AND geoCountryCode <> 'null') AS geo_country_iso,
  FIRSTWITHTIME(geoCity, "timestamp", 'STRING') FILTER (WHERE geoCity <> '' AND geoCity <> 'null') AS geo_city,
  min("timestamp") AS session_start,
  max("timestamp") AS session_end,
  min("timestamp") FILTER (WHERE eventName = 'faro.session_recording.started') AS session_replay_start
FROM faro_pinot_events_v2
WHERE appId = {{APP_ID}}
  AND sessionId = '{{SESSION_ID}}'
  AND $__timeFilter("timestamp")`

const pinotUserMetadataSQL = `SET useMultistageEngine = true;
SELECT
  FIRSTWITHTIME(userId, "timestamp", 'STRING') FILTER (WHERE userId <> '' AND userId <> 'null') AS user_id,
  FIRSTWITHTIME(userUsername, "timestamp", 'STRING') FILTER (WHERE userUsername <> '' AND userUsername <> 'null') AS user_username,
  FIRSTWITHTIME(userEmail, "timestamp", 'STRING') FILTER (WHERE userEmail <> '' AND userEmail <> 'null') AS user_email,
  FIRSTWITHTIME(appEnvironment, "timestamp", 'STRING') FILTER (WHERE appEnvironment <> '' AND appEnvironment <> 'null') AS app_environment,
  FIRSTWITHTIME(sdkName, "timestamp", 'STRING') FILTER (WHERE sdkName <> '' AND sdkName <> 'null') AS sdk_name,
  FIRSTWITHTIME(sdkVersion, "timestamp", 'STRING') FILTER (WHERE sdkVersion <> '' AND sdkVersion <> 'null') AS sdk_version,
  FIRSTWITHTIME(osName, "timestamp", 'STRING') FILTER (WHERE osName <> '' AND osName <> 'null') AS os_name,
  FIRSTWITHTIME(osVersion, "timestamp", 'STRING') FILTER (WHERE osVersion <> '' AND osVersion <> 'null') AS os_version,
  FIRSTWITHTIME(JSON_EXTRACT_SCALAR(attributesJson, '$[''device_model_name'']', 'STRING', ''), "timestamp", 'STRING') FILTER (WHERE JSON_EXTRACT_SCALAR(attributesJson, '$[''device_model_name'']', 'STRING', '') <> '') AS device_model_name,
  FIRSTWITHTIME(JSON_EXTRACT_SCALAR(attributesJson, '$[''device_manufacturer'']', 'STRING', ''), "timestamp", 'STRING') FILTER (WHERE JSON_EXTRACT_SCALAR(attributesJson, '$[''device_manufacturer'']', 'STRING', '') <> '') AS device_manufacturer,
  FIRSTWITHTIME(JSON_EXTRACT_SCALAR(attributesJson, '$[''device_brand'']', 'STRING', ''), "timestamp", 'STRING') FILTER (WHERE JSON_EXTRACT_SCALAR(attributesJson, '$[''device_brand'']', 'STRING', '') <> '') AS device_brand
FROM faro_pinot_measurements_v1
WHERE appId = {{APP_ID}}
  AND sessionId = '{{SESSION_ID}}'
  AND $__timeFilter("timestamp")`

// pinotJourneySQL is the session-detail user-journey UNION copied from Explore
// (web app 66 / qa8Fj6072u and mobile app 96 / kwwAkkXwas). Three legs:
// measurements, exceptions, events. The only mobile difference is
// {{MEASUREMENT_FILTER}} on the measurements WHERE. Outer columns are named
// (equivalent to Explore's SELECT *) so the SQL linter does not flag SELECT *.
const pinotJourneySQL = `SET useMultistageEngine = true;
SELECT
  "timestamp",
  kind,
  event_name,
  measurement_type,
  log_level,
  message,
  exception_type,
  exception_value_template,
  hash,
  attribute_hash,
  page_id,
  action_id,
  action_parent_id,
  action_name,
  traceID,
  session_id,
  http_status_code,
  http_method,
  http_url,
  component,
  duration_ns,
  userActionDuration,
  userActionImportance,
  userActionStartTime,
  userActionEndTime,
  userActionTrigger,
  web_vital_name,
  web_vital_value,
  mv_appStartDuration,
  mv_coldStart,
  mv_cpu_usage,
  mv_mem_usage,
  mv_refresh_rate,
  mv_slow_frames,
  mv_frozen_frames,
  to_view,
  from_view,
  nav_name,
  nav_duration,
  nav_ttfb
FROM (
  SELECT
    "timestamp" AS "timestamp",
    'measurement' AS kind,
    '' AS event_name,
    CASE WHEN measurementType = 'null' THEN '' ELSE measurementType END AS measurement_type,
    '' AS log_level,
    '' AS message,
    '' AS exception_type,
    '' AS exception_value_template,
    '' AS hash,
    '' AS attribute_hash,
    CASE WHEN pageId = 'null' THEN '' ELSE pageId END AS page_id,
    '' AS action_id,
    CASE WHEN actionParentId = 'null' THEN '' ELSE actionParentId END AS action_parent_id,
    CASE WHEN actionName = 'null' THEN '' ELSE actionName END AS action_name,
    '' AS traceID,
    sessionId AS session_id,
    '' AS http_status_code,
    '' AS http_method,
    '' AS http_url,
    '' AS component,
    '' AS duration_ns,
    '' AS userActionDuration,
    '' AS userActionImportance,
    '' AS userActionStartTime,
    '' AS userActionEndTime,
    '' AS userActionTrigger,
    CASE
      WHEN ttfb > 0 THEN 'ttfb'
      WHEN fcp > 0 THEN 'fcp'
      WHEN lcp > 0 THEN 'lcp'
      WHEN "measurementValues.cls" >= 0 THEN 'cls'
      WHEN inp > 0 THEN 'inp'
      ELSE ''
    END AS web_vital_name,
    CASE
      WHEN ttfb > 0 THEN CAST(ttfb AS VARCHAR)
      WHEN fcp > 0 THEN CAST(fcp AS VARCHAR)
      WHEN lcp > 0 THEN CAST(lcp AS VARCHAR)
      WHEN "measurementValues.cls" >= 0 THEN CAST("measurementValues.cls" AS VARCHAR)
      WHEN inp > 0 THEN CAST(inp AS VARCHAR)
      ELSE ''
    END AS web_vital_value,
    JSON_EXTRACT_SCALAR(measurementValues, '$[''appStartDuration'']', 'STRING', '') AS mv_appStartDuration,
    JSON_EXTRACT_SCALAR(measurementValues, '$[''coldStart'']', 'STRING', '') AS mv_coldStart,
    JSON_EXTRACT_SCALAR(measurementValues, '$[''cpu_usage'']', 'STRING', '') AS mv_cpu_usage,
    JSON_EXTRACT_SCALAR(measurementValues, '$[''mem_usage'']', 'STRING', '') AS mv_mem_usage,
    JSON_EXTRACT_SCALAR(measurementValues, '$[''refresh_rate'']', 'STRING', '') AS mv_refresh_rate,
    JSON_EXTRACT_SCALAR(measurementValues, '$[''slow_frames'']', 'STRING', '') AS mv_slow_frames,
    JSON_EXTRACT_SCALAR(measurementValues, '$[''frozen_frames'']', 'STRING', '') AS mv_frozen_frames,
    '' AS to_view,
    '' AS from_view,
    '' AS nav_name,
    '' AS nav_duration,
    '' AS nav_ttfb
  FROM faro_pinot_measurements_v1
  WHERE appId = {{APP_ID}}
    AND sessionId = '{{SESSION_ID}}'{{MEASUREMENT_FILTER}}
    AND $__timeFilter("timestamp")

  UNION ALL

  SELECT
    "timestamp" AS "timestamp",
    'exception' AS kind,
    '' AS event_name,
    '' AS measurement_type,
    '' AS log_level,
    '' AS message,
    CASE WHEN exceptionType = 'null' THEN '' ELSE exceptionType END AS exception_type,
    CASE WHEN exceptionValueTemplate = 'null' THEN '' ELSE exceptionValueTemplate END AS exception_value_template,
    exceptionHash AS hash,
    exceptionHash AS attribute_hash,
    CASE WHEN pageId = 'null' THEN '' ELSE pageId END AS page_id,
    '' AS action_id,
    CASE WHEN actionParentId = 'null' THEN '' ELSE actionParentId END AS action_parent_id,
    CASE WHEN actionName = 'null' THEN '' ELSE actionName END AS action_name,
    '' AS traceID,
    sessionId AS session_id,
    '' AS http_status_code,
    '' AS http_method,
    '' AS http_url,
    '' AS component,
    '' AS duration_ns,
    '' AS userActionDuration,
    '' AS userActionImportance,
    '' AS userActionStartTime,
    '' AS userActionEndTime,
    '' AS userActionTrigger,
    '' AS web_vital_name,
    '' AS web_vital_value,
    '' AS mv_appStartDuration,
    '' AS mv_coldStart,
    '' AS mv_cpu_usage,
    '' AS mv_mem_usage,
    '' AS mv_refresh_rate,
    '' AS mv_slow_frames,
    '' AS mv_frozen_frames,
    '' AS to_view,
    '' AS from_view,
    '' AS nav_name,
    '' AS nav_duration,
    '' AS nav_ttfb
  FROM faro_pinot_exceptions_v1
  WHERE appId = {{APP_ID}}
    AND sessionId = '{{SESSION_ID}}'
    AND $__timeFilter("timestamp")

  UNION ALL

  SELECT
    "timestamp" AS "timestamp",
    'event' AS kind,
    eventName AS event_name,
    '' AS measurement_type,
    '' AS log_level,
    '' AS message,
    '' AS exception_type,
    '' AS exception_value_template,
    '' AS hash,
    '' AS attribute_hash,
    CASE WHEN pageId = 'null' THEN '' ELSE pageId END AS page_id,
    '' AS action_id,
    CASE WHEN actionParentId = 'null' THEN '' ELSE actionParentId END AS action_parent_id,
    CASE WHEN actionName = 'null' THEN '' ELSE actionName END AS action_name,
    CASE WHEN traceId = 'null' THEN '' ELSE traceId END AS traceID,
    sessionId AS session_id,
    JSON_EXTRACT_SCALAR(attributesJson, '$[''http.status_code'']', 'STRING', '') AS http_status_code,
    JSON_EXTRACT_SCALAR(attributesJson, '$[''http.method'']', 'STRING', '') AS http_method,
    JSON_EXTRACT_SCALAR(attributesJson, '$[''http.url'']', 'STRING', '') AS http_url,
    JSON_EXTRACT_SCALAR(attributesJson, '$[''component'']', 'STRING', '') AS component,
    JSON_EXTRACT_SCALAR(attributesJson, '$[''duration_ns'']', 'STRING', '') AS duration_ns,
    JSON_EXTRACT_SCALAR(attributesJson, '$[''userActionDuration'']', 'STRING', '') AS userActionDuration,
    JSON_EXTRACT_SCALAR(attributesJson, '$[''userActionImportance'']', 'STRING', '') AS userActionImportance,
    JSON_EXTRACT_SCALAR(attributesJson, '$[''userActionStartTime'']', 'STRING', '') AS userActionStartTime,
    JSON_EXTRACT_SCALAR(attributesJson, '$[''userActionEndTime'']', 'STRING', '') AS userActionEndTime,
    JSON_EXTRACT_SCALAR(attributesJson, '$[''userActionTrigger'']', 'STRING', '') AS userActionTrigger,
    '' AS web_vital_name,
    '' AS web_vital_value,
    '' AS mv_appStartDuration,
    '' AS mv_coldStart,
    '' AS mv_cpu_usage,
    '' AS mv_mem_usage,
    '' AS mv_refresh_rate,
    '' AS mv_slow_frames,
    '' AS mv_frozen_frames,
    JSON_EXTRACT_SCALAR(attributesJson, '$[''toView'']', 'STRING', '') AS to_view,
    JSON_EXTRACT_SCALAR(attributesJson, '$[''fromView'']', 'STRING', '') AS from_view,
    JSON_EXTRACT_SCALAR(attributesJson, '$[''name'']', 'STRING', '') AS nav_name,
    JSON_EXTRACT_SCALAR(attributesJson, '$[''duration'']', 'STRING', '') AS nav_duration,
    JSON_EXTRACT_SCALAR(attributesJson, '$[''ttfb'']', 'STRING', '') AS nav_ttfb
  FROM faro_pinot_events_v2
  WHERE appId = {{APP_ID}}
    AND sessionId = '{{SESSION_ID}}'
    AND eventName NOT IN ('faro.performance.resource', 'faro.performanceEntry')
    AND $__timeFilter("timestamp")
) journey
ORDER BY "timestamp" ASC`

type sessionQueryParams struct {
	AppID     string
	SessionID string
	AppType   string
}

func (p sessionQueryParams) mobile() bool {
	return p.AppType == appTypeMobile
}

func substPinot(sql string, p sessionQueryParams) (string, error) {
	// appId is an unquoted numeric literal in the templates. EscapeSQLString
	// only doubles quotes, so a value like `66; DROP TABLE events` would
	// still inject. Canonicalize to a base-10 int64 or refuse to build SQL.
	appID, err := pinot.FormatSQLInt(p.AppID)
	if err != nil {
		return "", fmt.Errorf("invalid app id %q: must be an integer", p.AppID)
	}
	filter := ""
	if p.mobile() {
		filter = mobileMeasurementFilter
	}
	return strings.NewReplacer(
		"{{APP_ID}}", appID,
		"{{SESSION_ID}}", pinot.EscapeSQLString(p.SessionID),
		"{{MEASUREMENT_FILTER}}", filter,
	).Replace(sql), nil
}

func pinotEventsMetadataQuery(p sessionQueryParams) (string, error) {
	return substPinot(pinotEventsMetadataSQL, p)
}

func pinotUserMetadataQuery(p sessionQueryParams) (string, error) {
	return substPinot(pinotUserMetadataSQL, p)
}

func pinotJourneyQuery(p sessionQueryParams) (string, error) {
	return substPinot(pinotJourneySQL, p)
}

// inferAppType maps session telemetry to web vs mobile.
//
//	sdkName "faro-web" or empty + os "Mac OS"  → web
//	sdkName containing react-native / flutter → mobile
//	osName iOS / Android                      → mobile (sdk sometimes blank)
func inferAppType(sdkName, osName string) string {
	sdk := strings.ToLower(sdkName)
	os := strings.ToLower(osName)
	switch {
	case strings.Contains(sdk, "react-native"),
		strings.Contains(sdk, "flutter"),
		strings.Contains(sdk, "faro-android"),
		strings.Contains(sdk, "faro-ios"):
		return appTypeMobile
	case os == "ios", os == "android":
		return appTypeMobile
	default:
		return appTypeWeb
	}
}

func escapeLogQLString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func lokiMetadataQuery(p sessionQueryParams) string {
	app := escapeLogQLString(p.AppID)
	session := escapeLogQLString(p.SessionID)
	return fmt.Sprintf(
		`{app_id="%s", kind="event"} |= "session_id=%s" | logfmt | session_id="%s"`,
		app, session, session,
	)
}

func lokiReplayStartQuery(p sessionQueryParams) string {
	app := escapeLogQLString(p.AppID)
	session := escapeLogQLString(p.SessionID)
	return fmt.Sprintf(
		`{app_id="%s", kind="event"} |= "session_id=%s" |= "faro.session_recording.started" | logfmt | event_name="faro.session_recording.started" | session_id="%s"`,
		app, session, session,
	)
}

// lokiSessionEventKinds is the Pinot journey split: events, exceptions, logs,
// measurements as separate indexed Loki streams.
func lokiSessionEventKinds() []string {
	return []string{lokiKindEvent, lokiKindException, lokiKindLog, lokiKindMeasurement}
}

func lokiEventsQueryForKind(p sessionQueryParams, kind string) string {
	app := escapeLogQLString(p.AppID)
	session := escapeLogQLString(p.SessionID)
	q := fmt.Sprintf(
		`{app_id="%s", kind="%s"} |= "session_id=%s" !~ "performanceEntry|faro.performanceEntry|faro.performance.resource" | logfmt`,
		app, kind, session,
	)
	if kind == lokiKindMeasurement && p.mobile() {
		q += ` | type!="app_memory" | type!="app_cpu_usage"`
	}
	return q
}
