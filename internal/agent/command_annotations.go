package agent

import (
	"strings"

	"github.com/grafana/gcx/internal/docs"
	"github.com/spf13/cobra"
)

type annotation struct {
	Cost string // "small", "medium", or "large"
	Hint string // LLM scoping hint (required for medium/large)
}

// commandAnnotations maps command paths to their agent-facing metadata.
// This centralized registry ensures every leaf command has token_cost and
// (where appropriate) llm_hint annotations, enforced by consistency tests.
//
// Guidelines:
//   - small:  bounded output, single-resource reads, mutations, local ops
//   - medium: moderate data (status, timeline, schema output, filtered lists)
//   - large:  potentially unbounded output (get all resources, pull, query)
//   - Hint:   required for medium and large; shows how to narrow output
//
//nolint:gochecknoglobals // centralized annotation registry, accessed via ApplyAnnotations
var commandAnnotations = map[string]annotation{
	// -----------------------------------------------------------------------
	// Core CLI commands (cmd/gcx/)
	// -----------------------------------------------------------------------

	"gcx api": {Cost: "large", Hint: "Run gcx help-tree --depth 1 to discover dedicated commands. Prefer gcx slo, gcx metrics query, gcx logs query, gcx alert, etc. Reserve gcx api for endpoints without a dedicated command. Example: GET /api/health -o json"},

	// assistant
	"gcx assistant investigations approvals":         {Cost: "medium", Hint: "<id> -o json"},
	"gcx assistant investigations cancel":            {Cost: "small"},
	"gcx assistant investigations chat":              {Cost: "large", Hint: "<id> [--role=user|assistant|tool] [--include-hidden] -o json; full v2 chat thread with tool calls and results"},
	"gcx assistant investigations create":            {Cost: "small", Hint: "Use for deep cross-signal root cause analysis. Dispatches specialist agents for metrics, logs, traces, and profiles in parallel — more efficient than chaining individual gcx query commands. Example: --instruction=\"Checkout latency spike after deploy\""},
	"gcx assistant investigations document":          {Cost: "medium", Hint: "<investigation-id> <document-id> -o json"},
	"gcx assistant investigations get":               {Cost: "medium", Hint: "<id> -o json"},
	"gcx assistant investigations list":              {Cost: "small"},
	"gcx assistant investigations mode":              {Cost: "small"},
	"gcx assistant investigations narrative":         {Cost: "medium", Hint: "<id>; assistant-authored prose only (no tool plumbing)"},
	"gcx assistant investigations pause":             {Cost: "small"},
	"gcx assistant investigations regenerate-report": {Cost: "small"},
	"gcx assistant investigations report":            {Cost: "medium", Hint: "<id> -o json"},
	"gcx assistant investigations resume":            {Cost: "small"},
	"gcx assistant investigations share":             {Cost: "small", Hint: "<id> --team=<name> (repeatable)"},
	"gcx assistant investigations timeline":          {Cost: "medium", Hint: "<id> -o json"},
	"gcx assistant investigations todos":             {Cost: "medium", Hint: "<id> -o json"},
	"gcx assistant investigations tools":             {Cost: "medium", Hint: "<id> [--name=<tool>] -o json; tool calls with results"},
	"gcx assistant conversation list":                {Cost: "small", Hint: "Discover conversation IDs. --source assistant,slack,cli (default), --limit, --offset, -o json"},
	"gcx assistant conversation get":                 {Cost: "large", Hint: "Pull a conversation transcript by ID before continuing it with 'gcx assistant prompt --context-id'. Example: <conversation-id> -o json"},
	"gcx assistant mcp-servers create":               {Cost: "small"},
	"gcx assistant mcp-servers delete":               {Cost: "small"},
	"gcx assistant mcp-servers get":                  {Cost: "small"},
	"gcx assistant mcp-servers list":                 {Cost: "small"},
	"gcx assistant mcp-servers update":               {Cost: "small"},

	// login
	"gcx login":       {Cost: "small", Hint: "Browser OAuth (recommended for Grafana Cloud): gcx login <ctx> --server <url> --oauth — opens a browser for the user to approve; works in agent mode. Non-interactive token: gcx login <ctx> --yes --server <url> --token <grafana-sa-token> [--cloud-token <cap-token>]. Service-account tokens (--token) are created inside the Grafana instance — see " + docs.ServiceAccounts + ". Cloud access-policy tokens (--cloud-token) are created at grafana.com — see " + docs.AccessPolicies + ". Append .md to any grafana.com/docs URL to fetch markdown. Do not guess token URLs."},
	"gcx cloud login": {Cost: "small", Hint: "Authenticate to the Grafana Cloud platform API (grafana.com) for managing stacks and access policies, distinct from 'gcx login' which targets a single stack. Browser OAuth by default; non-interactive: gcx cloud login --cloud-token <cap-token>. Cloud access-policy tokens are created at grafana.com, see " + docs.AccessPolicies + "."},

	// commands
	"gcx commands": {Cost: "medium", Hint: "--flat -o json"},

	// config
	"gcx config check":           {Cost: "small"},
	"gcx config current-context": {Cost: "small"},
	"gcx config edit":            {Cost: "small"},
	"gcx config list-contexts":   {Cost: "small"},
	"gcx config path":            {Cost: "small"},
	"gcx config set":             {Cost: "small"},
	"gcx config unset":           {Cost: "small"},
	"gcx config use-context":     {Cost: "small"},
	"gcx config view":            {Cost: "medium", Hint: "--minify -o json"},

	// datasources
	"gcx datasources get":   {Cost: "medium", Hint: "<uid> -o json"},
	"gcx datasources list":  {Cost: "small"},
	"gcx datasources query": {Cost: "large", Hint: "Run gcx help-tree metrics (or logs, traces, profiles) to discover signal commands. Prefer gcx metrics query for PromQL, gcx logs query for LogQL, gcx traces query for TraceQL, gcx profiles query for profiling. Example: <datasource-uid> 'up' --since 1h -o json"},

	// datasources clickhouse
	"gcx datasources clickhouse query":          {Cost: "medium", Hint: "-d UID 'SELECT count() FROM events' -o json"},
	"gcx datasources clickhouse list-tables":    {Cost: "small"},
	"gcx datasources clickhouse describe-table": {Cost: "small", Hint: "TABLE -d UID --database default -o json"},

	// datasources athena
	"gcx datasources athena query":          {Cost: "medium", Hint: "-d UID 'SELECT count(*) FROM events' -o json"},
	"gcx datasources athena list-catalogs":  {Cost: "small", Hint: "-d UID -o json"},
	"gcx datasources athena list-databases": {Cost: "small", Hint: "-d UID --catalog AwsDataCatalog -o json"},
	"gcx datasources athena list-tables":    {Cost: "small", Hint: "-d UID --catalog AwsDataCatalog --database mydb -o json"},
	"gcx datasources athena describe-table": {Cost: "small", Hint: "TABLE -d UID --catalog AwsDataCatalog --database mydb -o json"},

	// datasources cloudwatch
	"gcx datasources cloudwatch query":           {Cost: "large", Hint: "gcx datasources cloudwatch query -d UID --region us-east-1 --namespace AWS/EC2 --metric CPUUtilization --since 1h -o json"},
	"gcx datasources cloudwatch list-namespaces": {Cost: "small", Hint: "gcx datasources cloudwatch list-namespaces -d UID --region us-east-1 -o json"},
	"gcx datasources cloudwatch list-metrics":    {Cost: "small", Hint: "gcx datasources cloudwatch list-metrics -d UID --region us-east-1 --namespace AWS/EC2 -o json"},
	"gcx datasources cloudwatch list-dimensions": {Cost: "small", Hint: "gcx datasources cloudwatch list-dimensions -d UID --region us-east-1 --namespace AWS/EC2 --metric CPUUtilization -o json"},
	"gcx datasources cloudwatch list-regions":    {Cost: "small", Hint: "gcx datasources cloudwatch list-regions -d UID -o json"},
	"gcx datasources cloudwatch list-accounts":   {Cost: "small", Hint: "gcx datasources cloudwatch list-accounts -d UID --region us-east-1 -o json"},

	// dev
	"gcx dev generate":   {Cost: "small"},
	"gcx dev import":     {Cost: "medium", Hint: "dashboards -p ./dashboards"},
	"gcx dev scaffold":   {Cost: "small"},
	"gcx dev serve":      {Cost: "small"},
	"gcx dev lint new":   {Cost: "small"},
	"gcx dev lint rules": {Cost: "small"},
	"gcx dev lint run":   {Cost: "medium", Hint: "./dashboards -o compact"},
	"gcx dev lint test":  {Cost: "medium", Hint: "./rules --run TestName"},

	// providers
	"gcx providers list": {Cost: "small"},

	// resources
	"gcx resources delete":   {Cost: "small"},
	"gcx resources edit":     {Cost: "small"},
	"gcx resources examples": {Cost: "small", Hint: "Docs: " + docs.DashboardJSONModel},
	"gcx resources get":      {Cost: "large", Hint: "dashboards/my-uid -o json"},
	"gcx resources pull":     {Cost: "large", Hint: "dashboards -p ./dashboards | Docs: " + docs.DashboardJSONModel},
	"gcx resources push":     {Cost: "medium", Hint: "-p ./dashboards --dry-run | Docs: " + docs.DashboardJSONModel},
	"gcx resources schemas":  {Cost: "small"},
	"gcx resources validate": {Cost: "medium", Hint: "-p ./dashboards | Docs: " + docs.DashboardJSONModel},

	// setup
	"gcx setup status": {Cost: "small"},

	// -----------------------------------------------------------------------
	// Instrumentation provider (action-verb tree — ADR-018)
	// -----------------------------------------------------------------------

	// clusters verb group
	"gcx instrumentation clusters list":      {Cost: "large", Hint: "-o json"},
	"gcx instrumentation clusters get":       {Cost: "medium", Hint: "<cluster> -o json"},
	"gcx instrumentation clusters configure": {Cost: "small"},
	"gcx instrumentation clusters remove":    {Cost: "small"},
	"gcx instrumentation clusters wait":      {Cost: "small"},

	// clusters apps verb group
	"gcx instrumentation clusters apps list":      {Cost: "medium", Hint: "<cluster> -o json"},
	"gcx instrumentation clusters apps get":       {Cost: "medium", Hint: "<cluster> <namespace> -o json"},
	"gcx instrumentation clusters apps configure": {Cost: "small"},
	"gcx instrumentation clusters apps remove":    {Cost: "small"},
	"gcx instrumentation clusters apps wait":      {Cost: "small"},

	// top-level single commands
	"gcx instrumentation setup":  {Cost: "medium", Hint: "<cluster> --use-defaults -o json | Docs: " + docs.KubernetesMonitoring},
	"gcx instrumentation status": {Cost: "medium", Hint: "-o json | Docs: " + docs.KubernetesMonitoring},
	"gcx instrumentation check":  {Cost: "small", Hint: "validates the LOCAL workstation's OTel setup (env vars, SDK deps, collector/Beyla/Alloy config, Grafana Cloud env creds) — does not query any Grafana stack. [components] --language <lang> -o json"},

	// services verb group
	"gcx instrumentation services list":    {Cost: "large", Hint: "K8s workloads discovered fleet-wide by the Beyla survey collector, for setting up instrumentation (distinct from 'gcx appo11y services', which lists telemetry-reporting services). --cluster <name> --namespace <ns> -o json"},
	"gcx instrumentation services get":     {Cost: "medium", Hint: "<cluster> <namespace> <service> -o json"},
	"gcx instrumentation services include": {Cost: "small"},
	"gcx instrumentation services exclude": {Cost: "small"},
	"gcx instrumentation services clear":   {Cost: "small"},

	// skills
	"gcx agent skills install":   {Cost: "small"},
	"gcx agent skills list":      {Cost: "small"},
	"gcx agent skills get":       {Cost: "small (medium for large skills)", Hint: "<name> [references/<file>]"},
	"gcx agent skills update":    {Cost: "small"},
	"gcx agent skills uninstall": {Cost: "small"},

	// -----------------------------------------------------------------------
	// Dashboards provider
	// -----------------------------------------------------------------------
	"gcx dashboards list":             {Cost: "medium", Hint: "-o json --api-version dashboard.grafana.app/v2"},
	"gcx dashboards get":              {Cost: "small", Hint: "<name> -o json"},
	"gcx dashboards create":           {Cost: "small", Hint: "-f <manifest.yaml>"},
	"gcx dashboards update":           {Cost: "small", Hint: "<name> -f <manifest.yaml>"},
	"gcx dashboards delete":           {Cost: "small"},
	"gcx dashboards search":           {Cost: "medium", Hint: "<query> -o json"},
	"gcx dashboards versions list":    {Cost: "small", Hint: "<name> -o json"},
	"gcx dashboards versions restore": {Cost: "small"},

	// -----------------------------------------------------------------------
	// Alert provider
	// -----------------------------------------------------------------------
	"gcx alert groups get":                   {Cost: "small"},
	"gcx alert groups list":                  {Cost: "small"},
	"gcx alert groups status":                {Cost: "medium", Hint: "<name> -o json"},
	"gcx alert instances list":               {Cost: "large", Hint: "--state firing --group <name> -o json"},
	"gcx alert rules get":                    {Cost: "small"},
	"gcx alert rules list":                   {Cost: "medium", Hint: "--folder <uid> --group <name> -o json"},
	"gcx alert contact-points list":          {Cost: "small"},
	"gcx alert contact-points get":           {Cost: "small"},
	"gcx alert contact-points create":        {Cost: "small"},
	"gcx alert contact-points update":        {Cost: "small"},
	"gcx alert contact-points delete":        {Cost: "small"},
	"gcx alert contact-points export":        {Cost: "medium", Hint: "--format yaml"},
	"gcx alert mute-timings list":            {Cost: "small"},
	"gcx alert mute-timings get":             {Cost: "small"},
	"gcx alert mute-timings create":          {Cost: "small"},
	"gcx alert mute-timings update":          {Cost: "small"},
	"gcx alert mute-timings delete":          {Cost: "small"},
	"gcx alert mute-timings export":          {Cost: "medium", Hint: "--format yaml [--name <mute-timing>]"},
	"gcx alert notification-policies get":    {Cost: "small"},
	"gcx alert notification-policies set":    {Cost: "small"},
	"gcx alert notification-policies reset":  {Cost: "small"},
	"gcx alert notification-policies export": {Cost: "medium", Hint: "--format yaml"},
	"gcx alert templates list":               {Cost: "small"},
	"gcx alert templates get":                {Cost: "small"},
	"gcx alert templates upsert":             {Cost: "small"},
	"gcx alert templates delete":             {Cost: "small"},

	// -----------------------------------------------------------------------
	// App Observability provider
	// -----------------------------------------------------------------------
	"gcx appo11y overrides get":    {Cost: "small"},
	"gcx appo11y overrides update": {Cost: "small"},
	"gcx appo11y settings get":     {Cost: "small"},
	"gcx appo11y settings update":  {Cost: "small"},

	// -----------------------------------------------------------------------
	// Frontend Observability provider
	// -----------------------------------------------------------------------
	"gcx frontend apps apply-sourcemap":  {Cost: "small", Hint: "<app-name> -f <sourcemap-file>"},
	"gcx frontend apps create":           {Cost: "small"},
	"gcx frontend apps delete":           {Cost: "small"},
	"gcx frontend apps get":              {Cost: "small"},
	"gcx frontend apps list":             {Cost: "small"},
	"gcx frontend apps remove-sourcemap": {Cost: "small"},
	"gcx frontend apps show-sourcemaps":  {Cost: "small"},
	"gcx frontend apps update":           {Cost: "small"},

	// -----------------------------------------------------------------------
	// Fleet provider
	// -----------------------------------------------------------------------
	"gcx fleet collectors create": {Cost: "small", Hint: "-f <manifest.yaml>"},
	"gcx fleet collectors delete": {Cost: "small"},
	"gcx fleet collectors get":    {Cost: "small"},
	"gcx fleet collectors list":   {Cost: "small"},
	"gcx fleet collectors update": {Cost: "small"},
	"gcx fleet pipelines create":  {Cost: "small", Hint: "-f <manifest.yaml>"},
	"gcx fleet pipelines delete":  {Cost: "small"},
	"gcx fleet pipelines get":     {Cost: "small"},
	"gcx fleet pipelines list":    {Cost: "small"},
	"gcx fleet pipelines update":  {Cost: "small"},
	"gcx fleet tenant limits":     {Cost: "small"},

	// -----------------------------------------------------------------------
	// IRM Incidents
	// -----------------------------------------------------------------------
	"gcx irm incidents activity add":    {Cost: "small"},
	"gcx irm incidents activity list":   {Cost: "small"},
	"gcx irm incidents close":           {Cost: "small"},
	"gcx irm incidents contexts list":   {Cost: "small"},
	"gcx irm incidents create":          {Cost: "small"},
	"gcx irm incidents get":             {Cost: "small"},
	"gcx irm incidents list":            {Cost: "small"},
	"gcx irm incidents open":            {Cost: "small"},
	"gcx irm incidents severities list": {Cost: "small"},

	// -----------------------------------------------------------------------
	// k6 provider
	// -----------------------------------------------------------------------
	"gcx k6 auth token":                           {Cost: "small"},
	"gcx k6 env-vars create":                      {Cost: "small", Hint: "-f <manifest.yaml>"},
	"gcx k6 env-vars delete":                      {Cost: "small"},
	"gcx k6 env-vars list":                        {Cost: "small"},
	"gcx k6 env-vars update":                      {Cost: "small"},
	"gcx k6 load-tests create":                    {Cost: "small", Hint: "-f <manifest.yaml>. Running the created test consumes billable VU hours (" + docs.PerformanceTestingInvoice + ")."},
	"gcx k6 load-tests delete":                    {Cost: "small"},
	"gcx k6 load-tests get":                       {Cost: "small", Hint: "<id-or-name> [--project-id <id>]"},
	"gcx k6 load-tests list":                      {Cost: "small"},
	"gcx k6 load-tests update":                    {Cost: "small"},
	"gcx k6 load-tests update-script":             {Cost: "small"},
	"gcx k6 load-zones allowed-load-zones list":   {Cost: "small"},
	"gcx k6 load-zones allowed-load-zones update": {Cost: "small"},
	"gcx k6 load-zones allowed-projects list":     {Cost: "small"},
	"gcx k6 load-zones allowed-projects update":   {Cost: "small"},
	"gcx k6 load-zones create":                    {Cost: "small"},
	"gcx k6 load-zones delete":                    {Cost: "small"},
	"gcx k6 load-zones list":                      {Cost: "small"},
	"gcx k6 projects create":                      {Cost: "small", Hint: "-f <manifest.yaml>"},
	"gcx k6 projects delete":                      {Cost: "small"},
	"gcx k6 projects get":                         {Cost: "small"},
	"gcx k6 projects list":                        {Cost: "small"},
	"gcx k6 projects update":                      {Cost: "small"},
	"gcx k6 runs list":                            {Cost: "small"},
	"gcx k6 schedules create":                     {Cost: "small", Hint: "-f <manifest.yaml>"},
	"gcx k6 schedules delete":                     {Cost: "small"},
	"gcx k6 schedules get":                        {Cost: "small"},
	"gcx k6 schedules list":                       {Cost: "small"},
	"gcx k6 schedules update":                     {Cost: "small"},
	"gcx k6 test-run emit":                        {Cost: "small", Hint: "[test-name] --project-id <id> [--apply]. --apply starts a run that consumes billable VU hours (" + docs.PerformanceTestingInvoice + ")."},
	"gcx k6 test-run runs list":                   {Cost: "small"},
	"gcx k6 test-run status":                      {Cost: "small"},

	// -----------------------------------------------------------------------
	// Knowledge Graph provider
	// -----------------------------------------------------------------------
	"gcx kg entities query":       {Cost: "medium", Hint: "\"MATCH (s:Service) RETURN s LIMIT 10\" [--since 1h] | read-only Cypher query; always include LIMIT for targeted lookups; omit for broad discovery"},
	"gcx kg diagnose":             {Cost: "medium", Hint: "[--env <env>] | run diagnostic checks on the Knowledge Graph pipeline: stack status, sanity checks, entity counts, scopes, telemetry configs, recording rule metrics"},
	"gcx kg diagnose service":     {Cost: "medium", Hint: "<service-name> [--env <env>] | deep diagnosis for a specific service: entity lookup, relationships, per-service metrics, interpreted diagnosis with next steps"},
	"gcx kg diagnose labels":      {Cost: "medium", Hint: "validate the deployment_environment → asserts_env label mapping pipeline | identifies unmapped environments and orphaned asserts_env values"},
	"gcx kg entities list":        {Cost: "medium", Hint: "--type <type> [--env <env>] [--namespace <ns>] --since 1h -o json | use --property name=<value> to fetch a single entity by name | use --insight severity=critical|warning|info or --insight name=<assertion> to filter to entities with active insights (repeatable; matchers AND on the same assertion) | use --json type,name,scope when only entity identity is needed to reduce output size | run gcx kg meta scopes first to discover valid env/namespace/site values | 'gcx appo11y services list' provides a telemetry-based App Observability service inventory (instrumentation coverage) that does not require the Knowledge Graph"},
	"gcx kg summary":              {Cost: "medium", Hint: "--type <type> --since 1h -o json"},
	"gcx kg insights chart":       {Cost: "medium", Hint: "<Type--Name> --insight <name> [--label k=v ...] | returns time series + thresholds backing the insight chart"},
	"gcx kg insights sources":     {Cost: "medium", Hint: "<Type--Name> --insight <name> [--label k=v ...] --since 1h | returns the underlying PromQL metric names and label matchers sourcing the insight"},
	"gcx kg entities inspect":     {Cost: "medium", Hint: "--type <EntityType> --name <name> [--env <env>] [--namespace <ns>] | --type is required (run 'gcx kg meta schema' to list valid entity types); scope is auto-discovered if omitted; run 'gcx kg entities list --type <type> --property name=~<name>' first to confirm type and exact name"},
	"gcx kg meta all":             {Cost: "medium", Hint: "load all sections at once [--since 1h]"},
	"gcx kg meta logs":            {Cost: "small", Hint: "Loki label mappings for log drilldown"},
	"gcx kg meta metrics":         {Cost: "small", Hint: "asserts:* KPI catalog — Knowledge Graph recording rules (rate/error/latency, cpu/memory) + entity-property→Prometheus-label mapping for building selectors"},
	"gcx kg meta profiles":        {Cost: "small", Hint: "Pyroscope label mappings for profile drilldown"},
	"gcx kg meta schema":          {Cost: "medium", Hint: "entity types + relationships [--since 1h]"},
	"gcx kg meta scopes":          {Cost: "small", Hint: "all valid env/namespace/site filter values — run before filtering entities"},
	"gcx kg meta traces":          {Cost: "small", Hint: "Tempo label mappings for trace drilldown"},
	"gcx kg model-rules create":   {Cost: "small"},
	"gcx kg model-rules delete":   {Cost: "small"},
	"gcx kg model-rules get":      {Cost: "small"},
	"gcx kg model-rules list":     {Cost: "small"},
	"gcx kg model-rules schema":   {Cost: "small", Hint: "live JSON Schema for ModelRules from backend — pipe to file for editor autocomplete"},
	"gcx kg open":                 {Cost: "small"},
	"gcx kg relabel-rules get":    {Cost: "small", Hint: "fetch prologue, epilogue, or generated Mimir relabel rules [--type generated]"},
	"gcx kg prom-rules create":    {Cost: "small"},
	"gcx kg prom-rules delete":    {Cost: "small"},
	"gcx kg prom-rules get":       {Cost: "small"},
	"gcx kg prom-rules list":      {Cost: "small"},
	"gcx kg prom-rules schema":    {Cost: "small", Hint: "live JSON Schema for Custom Prometheus rules from backend — pipe to file for editor autocomplete, or validate prom-rules YAML before create"},
	"gcx kg status":               {Cost: "small"},
	"gcx kg suppressions create":  {Cost: "small", Hint: "-f suppressions.yaml (or pipe YAML via stdin) | upsert one or more alert suppressions; add --dry-run to validate against the backend and preview the remote->local diff without uploading"},
	"gcx kg suppressions delete":  {Cost: "small"},
	"gcx kg suppressions list":    {Cost: "small"},
	"gcx kg entities create":      {Cost: "small", Hint: "--domain <d> --type <Type> --name <n> [--scope k=v] [--property k=v] [--ttl 1h] | upsert a custom (API-origin) entity; experimental (KG write API, gated server-side)"},
	"gcx kg entities delete":      {Cost: "small", Hint: "<Type--Name> --domain <d> [--scope k=v] [--force] | delete a custom entity; --scope must match the value used at create; experimental (KG write API, gated server-side)"},
	"gcx kg relationships create": {Cost: "small", Hint: "--type <REL> --domain <d> --from <domain/Type/name> --to <domain/Type/name> [--from-scope k=v] [--to-scope k=v] [--ttl 1h] | upsert a custom edge; both endpoints must already exist; experimental (KG write API, gated server-side)"},
	"gcx kg relationships delete": {Cost: "small", Hint: "--type <REL> --from <domain/Type/name> --to <domain/Type/name> [--force] | delete a custom edge; --from/--to must match the values used at create; experimental (KG write API, gated server-side)"},

	// -----------------------------------------------------------------------
	// Logs provider
	// -----------------------------------------------------------------------
	"gcx logs labels":  {Cost: "small"},
	"gcx logs metrics": {Cost: "large", Hint: "'rate({job=\"myapp\"}[5m])' --since 1h -o json"},
	"gcx logs query":   {Cost: "large", Hint: "'{job=\"myapp\"}' --since 1h --limit 100 -o json | Docs: " + docs.LogQL},
	"gcx logs series":  {Cost: "medium", Hint: "--match '{job=\"myapp\"}' -o json"},

	// Logs adaptive
	"gcx logs adaptive drop-rules create": {Cost: "small"},
	"gcx logs adaptive drop-rules delete": {Cost: "small"},
	"gcx logs adaptive drop-rules get":    {Cost: "small"},
	"gcx logs adaptive drop-rules list":   {Cost: "small", Hint: "Docs: " + docs.AdaptiveLogs},
	"gcx logs adaptive drop-rules update": {Cost: "small"},
	"gcx logs adaptive exemptions create": {Cost: "small"},
	"gcx logs adaptive exemptions delete": {Cost: "small"},
	"gcx logs adaptive exemptions list":   {Cost: "small"},
	"gcx logs adaptive exemptions update": {Cost: "small"},
	"gcx logs adaptive patterns show":     {Cost: "small"},
	"gcx logs adaptive patterns stats":    {Cost: "small"},
	"gcx logs adaptive segments create":   {Cost: "small"},
	"gcx logs adaptive segments delete":   {Cost: "small"},
	"gcx logs adaptive segments list":     {Cost: "small"},
	"gcx logs adaptive segments update":   {Cost: "small"},

	// -----------------------------------------------------------------------
	// Metrics provider
	// -----------------------------------------------------------------------
	"gcx metrics labels":   {Cost: "small"},
	"gcx metrics metadata": {Cost: "medium", Hint: "--metric <name> -o json"},
	"gcx metrics query":    {Cost: "large", Hint: "'up' --since 1h -o json | Docs: " + docs.PromQL},

	// Metrics adaptive
	"gcx metrics adaptive recommendations apply": {Cost: "small"},
	"gcx metrics adaptive recommendations diff":  {Cost: "medium", Hint: "<metric> -o json"},
	"gcx metrics adaptive recommendations show":  {Cost: "small", Hint: "Docs: " + docs.AdaptiveMetrics},
	"gcx metrics adaptive rules create":          {Cost: "small"},
	"gcx metrics adaptive rules delete":          {Cost: "small"},
	"gcx metrics adaptive rules get":             {Cost: "small"},
	"gcx metrics adaptive rules list":            {Cost: "small"},
	"gcx metrics adaptive rules update":          {Cost: "small"},
	"gcx metrics adaptive segments create":       {Cost: "small"},
	"gcx metrics adaptive segments delete":       {Cost: "small"},
	"gcx metrics adaptive segments get":          {Cost: "small"},
	"gcx metrics adaptive segments list":         {Cost: "small"},
	"gcx metrics adaptive segments update":       {Cost: "small"},
	"gcx metrics adaptive exemptions create":     {Cost: "small"},
	"gcx metrics adaptive exemptions delete":     {Cost: "small"},
	"gcx metrics adaptive exemptions get":        {Cost: "small"},
	"gcx metrics adaptive exemptions list":       {Cost: "small"},
	"gcx metrics adaptive exemptions update":     {Cost: "small"},

	// -----------------------------------------------------------------------
	// IRM OnCall
	// -----------------------------------------------------------------------
	// alert-groups discovery: cost annotations
	// reflect the cap/filter shape; hints surface the rich-shape pivots and
	// the opt-out flags so the agent picks the right verb on the first try.
	"gcx irm oncall alert-groups list": {
		Cost: "small (large with --all)",
		Hint: "Defaults to firing+acknowledged+silenced root groups (matches OnCall UI). Use --all to include resolved + child groups (much larger). Filter with --state, --team, --integration, --max-age, --mine. Example: --state firing --team prod-sre.",
	},
	"gcx irm oncall alert-groups get": {
		Cost: "small",
		Hint: "Returns the rich K8s envelope with status.links.alert.rule.uid (pivot to `gcx alert instances list --rule <uid>`), status.links.dashboard.uid (pivot to `gcx resources get dashboards/<uid>`), and status.subject.labels (denylist-filtered commonLabels). list-alerts is unnecessary for typical drilldowns — get already carries the cross-provider pivots.",
	},
	"gcx irm oncall alert-groups list-alerts": {
		Cost: "medium (small with --slim)",
		Hint: "Rich-by-default: N+1 per-alert retrieves under bounded concurrency populate status.links.alert.rule.uid and status.links.dashboard.uid for each alert. Pass --slim to skip the N+1 and return slim envelopes (no links, no dimensions). Pass --history to opt out of collapse-by-label-set. 100-alert cap; --limit 0 removes it.",
	},
	// alert-groups action verbs: bulk-by-filter composes with the
	// same flag set as `list`; agent mode requires --yes when count > 1.
	"gcx irm oncall alert-groups acknowledge": {
		Cost: "small (medium with broad filter)",
		Hint: "Single-target: pass <id>. Bulk-by-filter: pass --team / --state / --integration / --max-age / --mine (same names as `alert-groups list`). Agent mode requires --yes when the matched count > 1 (no auto-confirm of destructive bulk operations). Idempotent: re-running on an already-acked group reports changed:false.",
	},
	"gcx irm oncall alert-groups unacknowledge": {
		Cost: "small (medium with broad filter)",
		Hint: "Reverts acknowledge → firing. Filters mirror `alert-groups list`. Agent mode requires --yes when count > 1. Idempotent: already-firing groups report changed:false.",
	},
	"gcx irm oncall alert-groups resolve": {
		Cost: "small (medium with broad filter)",
		Hint: "Closes the alert group (firing → resolved). Filters mirror `alert-groups list`. Agent mode requires --yes when count > 1. Idempotent: already-resolved groups report changed:false.",
	},
	"gcx irm oncall alert-groups unresolve": {
		Cost: "small (medium with broad filter)",
		Hint: "Reverts resolve → firing. Filters mirror `alert-groups list`. Agent mode requires --yes when count > 1. Idempotent: already-firing groups report changed:false.",
	},
	"gcx irm oncall alert-groups silence": {
		Cost: "small (medium with broad filter)",
		Hint: "Mutes the group for --duration seconds (default 3600). Filters mirror `alert-groups list`. Agent mode requires --yes when count > 1. Idempotent: already-silenced groups report changed:false.",
	},
	"gcx irm oncall alert-groups unsilence": {
		Cost: "small (medium with broad filter)",
		Hint: "Reverts silence → firing. Filters mirror `alert-groups list`. Agent mode requires --yes when count > 1. Idempotent: already-firing groups report changed:false.",
	},
	"gcx irm oncall alert-groups delete": {
		Cost: "small (medium with broad filter)",
		Hint: "Destructive: removes the alert group entirely (no idempotent skip). Filters mirror `alert-groups list`. Agent mode requires --yes when count > 1.",
	},
	"gcx irm oncall escalate":                       {Cost: "small", Hint: "--title \"title\" --user-ids id1,id2"},
	"gcx irm oncall escalation-chains create":       {Cost: "small", Hint: "-f chain.yaml"},
	"gcx irm oncall escalation-chains delete":       {Cost: "small"},
	"gcx irm oncall escalation-chains get":          {Cost: "small"},
	"gcx irm oncall escalation-chains list":         {Cost: "small"},
	"gcx irm oncall escalation-chains update":       {Cost: "small", Hint: "<id> -f chain.yaml"},
	"gcx irm oncall escalation-policies create":     {Cost: "small", Hint: "-f policy.yaml (run `escalation-policies steps list` for allowed step values)"},
	"gcx irm oncall escalation-policies delete":     {Cost: "small"},
	"gcx irm oncall escalation-policies get":        {Cost: "small"},
	"gcx irm oncall escalation-policies list":       {Cost: "small"},
	"gcx irm oncall escalation-policies steps list": {Cost: "small"},
	"gcx irm oncall escalation-policies update":     {Cost: "small", Hint: "<id> -f policy.yaml"},
	"gcx irm oncall integrations create":            {Cost: "small", Hint: "-f integration.yaml"},
	"gcx irm oncall integrations delete":            {Cost: "small"},
	"gcx irm oncall integrations get":               {Cost: "small"},
	"gcx irm oncall integrations list":              {Cost: "small"},
	"gcx irm oncall integrations update":            {Cost: "small", Hint: "<id> -f integration.yaml"},
	"gcx irm oncall organizations get":              {Cost: "small"},
	"gcx irm oncall resolution-notes create":        {Cost: "small", Hint: "-f note.yaml"},
	"gcx irm oncall resolution-notes delete":        {Cost: "small"},
	"gcx irm oncall resolution-notes get":           {Cost: "small"},
	"gcx irm oncall resolution-notes list":          {Cost: "small"},
	"gcx irm oncall resolution-notes update":        {Cost: "small", Hint: "<id> -f note.yaml"},
	"gcx irm oncall routes create":                  {Cost: "small", Hint: "-f route.yaml (run `routes filter-types list` for filtering_term_type values)"},
	"gcx irm oncall routes delete":                  {Cost: "small"},
	"gcx irm oncall routes filter-types list":       {Cost: "small"},
	"gcx irm oncall routes get":                     {Cost: "small"},
	"gcx irm oncall routes list":                    {Cost: "small"},
	"gcx irm oncall routes update":                  {Cost: "small", Hint: "<id> -f route.yaml"},
	"gcx irm oncall schedules create":               {Cost: "small", Hint: "-f schedule.yaml"},
	"gcx irm oncall schedules delete":               {Cost: "small"},
	"gcx irm oncall schedules final-shifts":         {Cost: "medium", Hint: "<schedule-id> --start 2024-01-01 --end 2024-01-31 -o json"},
	"gcx irm oncall schedules get":                  {Cost: "small"},
	"gcx irm oncall schedules list":                 {Cost: "small"},
	"gcx irm oncall schedules update":               {Cost: "small", Hint: "<id> -f schedule.yaml"},
	"gcx irm oncall shift-swaps create":             {Cost: "small", Hint: "-f swap.yaml"},
	"gcx irm oncall shift-swaps delete":             {Cost: "small"},
	"gcx irm oncall shift-swaps get":                {Cost: "small"},
	"gcx irm oncall shift-swaps list":               {Cost: "small"},
	"gcx irm oncall shift-swaps update":             {Cost: "small", Hint: "<id> -f swap.yaml"},
	"gcx irm oncall shifts create":                  {Cost: "small", Hint: "-f shift.yaml"},
	"gcx irm oncall shifts delete":                  {Cost: "small"},
	"gcx irm oncall shifts get":                     {Cost: "small"},
	"gcx irm oncall shifts list":                    {Cost: "small"},
	"gcx irm oncall shifts update":                  {Cost: "small", Hint: "<id> -f shift.yaml"},
	"gcx irm oncall slack-channels list":            {Cost: "small"},
	"gcx irm oncall teams get":                      {Cost: "small"},
	"gcx irm oncall teams list":                     {Cost: "small"},
	"gcx irm oncall user-groups list":               {Cost: "small"},
	"gcx irm oncall users current":                  {Cost: "small"},
	"gcx irm oncall users get":                      {Cost: "small"},
	"gcx irm oncall users list":                     {Cost: "small"},
	"gcx irm oncall webhooks create":                {Cost: "small", Hint: "-f webhook.yaml (run `webhooks triggers list` and `webhooks presets list` for allowed values)"},
	"gcx irm oncall webhooks delete":                {Cost: "small"},
	"gcx irm oncall webhooks get":                   {Cost: "small"},
	"gcx irm oncall webhooks list":                  {Cost: "small"},
	"gcx irm oncall webhooks presets list":          {Cost: "small"},
	"gcx irm oncall webhooks triggers list":         {Cost: "small"},
	"gcx irm oncall webhooks update":                {Cost: "small", Hint: "<id> -f webhook.yaml"},

	// -----------------------------------------------------------------------
	// Profiles provider
	// -----------------------------------------------------------------------
	"gcx profiles adaptive":           {Cost: "small"},
	"gcx profiles labels":             {Cost: "small"},
	"gcx profiles list-profile-types": {Cost: "small"},
	"gcx profiles metrics":            {Cost: "large", Hint: "'{service_name=\"frontend\"}' --profile-type cpu --since 1h -o json"},
	"gcx profiles query":              {Cost: "large", Hint: "'{service_name=\"frontend\"}' --profile-type cpu --since 1h -o json | Docs: " + docs.PyroscopeQueries},

	// -----------------------------------------------------------------------
	// Agent Observability provider
	// -----------------------------------------------------------------------
	"gcx agento11y agents get":      {Cost: "small"},
	"gcx agento11y agents list":     {Cost: "small"},
	"gcx agento11y agents versions": {Cost: "small"},

	"gcx agento11y conversations annotate":         {Cost: "small"},
	"gcx agento11y conversations get":              {Cost: "medium", Hint: "<conversation-id> -o json"},
	"gcx agento11y conversations list":             {Cost: "small"},
	"gcx agento11y conversations list-annotations": {Cost: "small"},
	"gcx agento11y conversations search":           {Cost: "medium", Hint: "--from 2024-01-01 --to 2024-01-31 -o json"},

	"gcx agento11y evaluators create": {Cost: "small"},
	"gcx agento11y evaluators delete": {Cost: "small"},
	"gcx agento11y evaluators get":    {Cost: "small"},
	"gcx agento11y evaluators list":   {Cost: "small"},
	"gcx agento11y evaluators test":   {Cost: "medium", Hint: "<evaluator-id> -o json"},

	"gcx agento11y generations get": {Cost: "medium", Hint: "<generation-id> -o json"},

	"gcx agento11y guards create": {Cost: "small"},
	"gcx agento11y guards delete": {Cost: "small"},
	"gcx agento11y guards get":    {Cost: "small"},
	"gcx agento11y guards list":   {Cost: "small"},
	"gcx agento11y guards update": {Cost: "small"},

	"gcx agento11y judge models":    {Cost: "small"},
	"gcx agento11y judge providers": {Cost: "small"},

	"gcx agento11y rules create": {Cost: "small"},
	"gcx agento11y rules delete": {Cost: "small"},
	"gcx agento11y rules get":    {Cost: "small"},
	"gcx agento11y rules list":   {Cost: "small"},
	"gcx agento11y rules update": {Cost: "small"},

	"gcx agento11y scores list": {Cost: "small"},

	"gcx agento11y templates get":      {Cost: "small"},
	"gcx agento11y templates list":     {Cost: "small"},
	"gcx agento11y templates versions": {Cost: "small"},

	"gcx agento11y saved-conversations list":        {Cost: "small"},
	"gcx agento11y saved-conversations get":         {Cost: "medium", Hint: "<saved-id> -o json"},
	"gcx agento11y saved-conversations save":        {Cost: "small"},
	"gcx agento11y saved-conversations delete":      {Cost: "small"},
	"gcx agento11y saved-conversations collections": {Cost: "small"},

	"gcx agento11y collections list":                 {Cost: "small"},
	"gcx agento11y collections get":                  {Cost: "small"},
	"gcx agento11y collections create":               {Cost: "small"},
	"gcx agento11y collections update":               {Cost: "small"},
	"gcx agento11y collections delete":               {Cost: "small"},
	"gcx agento11y collections conversations list":   {Cost: "small"},
	"gcx agento11y collections conversations add":    {Cost: "small"},
	"gcx agento11y collections conversations remove": {Cost: "small"},

	"gcx agento11y experiments list":   {Cost: "small"},
	"gcx agento11y experiments get":    {Cost: "small"},
	"gcx agento11y experiments create": {Cost: "small"},
	"gcx agento11y experiments update": {Cost: "small"},
	"gcx agento11y experiments cancel": {Cost: "small"},
	"gcx agento11y experiments scores": {Cost: "medium", Hint: "<run-id> --limit 50 -o json"},
	"gcx agento11y experiments report": {Cost: "medium", Hint: "<run-id> -o json"},

	// -----------------------------------------------------------------------
	// SLO provider
	// -----------------------------------------------------------------------
	"gcx slo definitions delete":   {Cost: "small"},
	"gcx slo definitions get":      {Cost: "small"},
	"gcx slo definitions list":     {Cost: "medium", Hint: "--limit 50 -o json"},
	"gcx slo definitions pull":     {Cost: "medium", Hint: "-d ./slo-definitions"},
	"gcx slo definitions push":     {Cost: "medium", Hint: "./definitions.yaml --dry-run"},
	"gcx slo definitions status":   {Cost: "medium", Hint: "<uuid> -o json"},
	"gcx slo definitions timeline": {Cost: "medium", Hint: "<uuid> --since 7d -o json"},
	"gcx slo reports delete":       {Cost: "small"},
	"gcx slo reports get":          {Cost: "small"},
	"gcx slo reports list":         {Cost: "small"},
	"gcx slo reports pull":         {Cost: "medium", Hint: "-d ./slo-reports"},
	"gcx slo reports push":         {Cost: "medium", Hint: "./reports.yaml --dry-run"},
	"gcx slo reports status":       {Cost: "medium", Hint: "<uuid> -o json"},
	"gcx slo reports timeline":     {Cost: "medium", Hint: "<uuid> --since 7d -o json"},

	// -----------------------------------------------------------------------
	// Synthetic Monitoring provider
	// -----------------------------------------------------------------------
	"gcx synthetic-monitoring checks create":      {Cost: "small", Hint: "-f <check.yaml>. Creates a billable check: each test execution and its resulting metrics/logs count toward Grafana Cloud usage (" + docs.SyntheticMonitoringInvoice + ")."},
	"gcx synthetic-monitoring checks delete":      {Cost: "small"},
	"gcx synthetic-monitoring checks get":         {Cost: "small"},
	"gcx synthetic-monitoring checks list":        {Cost: "small"},
	"gcx synthetic-monitoring checks status":      {Cost: "medium", Hint: "--job <name> -o json"},
	"gcx synthetic-monitoring checks timeline":    {Cost: "medium", Hint: "<id> --since 1h -o json"},
	"gcx synthetic-monitoring checks update":      {Cost: "small", Hint: "<name> -f <check.yaml>. Frequency and probe changes affect billable execution volume (" + docs.SyntheticMonitoringInvoice + ")."},
	"gcx synthetic-monitoring probes create":      {Cost: "small"},
	"gcx synthetic-monitoring probes delete":      {Cost: "small"},
	"gcx synthetic-monitoring probes deploy":      {Cost: "small"},
	"gcx synthetic-monitoring probes list":        {Cost: "small"},
	"gcx synthetic-monitoring probes token-reset": {Cost: "small"},

	// -----------------------------------------------------------------------
	// Traces provider
	// -----------------------------------------------------------------------
	"gcx traces get":     {Cost: "large", Hint: "<trace-id> --llm -o json"},
	"gcx traces labels":  {Cost: "small"},
	"gcx traces metrics": {Cost: "large", Hint: "'rate({ span.http.status_code >= 500 }[5m])' --since 1h -o json"},
	"gcx traces query":   {Cost: "large", Hint: "'{ span.http.status_code >= 500 }' --since 1h --limit 20 -o json | Docs: " + docs.TraceQL},

	// Traces adaptive
	"gcx traces adaptive policies create":         {Cost: "small"},
	"gcx traces adaptive policies delete":         {Cost: "small"},
	"gcx traces adaptive policies get":            {Cost: "small"},
	"gcx traces adaptive policies list":           {Cost: "small"},
	"gcx traces adaptive policies update":         {Cost: "small"},
	"gcx traces adaptive recommendations apply":   {Cost: "small"},
	"gcx traces adaptive recommendations dismiss": {Cost: "small"},
	"gcx traces adaptive recommendations show":    {Cost: "small", Hint: "Docs: " + docs.AdaptiveTraces},
}

// ApplyAnnotations walks the command tree and applies agent annotations from
// the centralized registry. Existing annotations on a command are preserved;
// registry entries only fill in missing keys. Call this after the full command
// tree is assembled.
func ApplyAnnotations(root *cobra.Command) {
	WalkCommands(root, func(cmd *cobra.Command) {
		a, ok := commandAnnotations[cmd.CommandPath()]
		if !ok {
			return
		}
		if cmd.Annotations == nil {
			cmd.Annotations = make(map[string]string)
		}
		if _, exists := cmd.Annotations[AnnotationTokenCost]; !exists && a.Cost != "" {
			cmd.Annotations[AnnotationTokenCost] = a.Cost
		}
		if _, exists := cmd.Annotations[AnnotationLLMHint]; !exists && a.Hint != "" {
			cmd.Annotations[AnnotationLLMHint] = a.Hint
		}
	})

	// Set the related-skill annotation on each mapped area node.
	WalkCommands(root, func(cmd *cobra.Command) {
		skills, ok := commandSkills[cmd.CommandPath()]
		if !ok || len(skills) == 0 {
			return
		}
		if cmd.Annotations == nil {
			cmd.Annotations = make(map[string]string)
		}
		if _, exists := cmd.Annotations[AnnotationSkill]; !exists {
			cmd.Annotations[AnnotationSkill] = strings.Join(skills, ",")
		}
	})

	// Mark Grafana Cloud-only commands. Derived from the command path so the
	// whole subtree of each cloud-only product group is covered at once.
	WalkCommands(root, func(cmd *cobra.Command) {
		if !IsCloudOnlyPath(cmd.CommandPath()) {
			return
		}
		if cmd.Annotations == nil {
			cmd.Annotations = make(map[string]string)
		}
		if _, exists := cmd.Annotations[AnnotationAvailability]; !exists {
			cmd.Annotations[AnnotationAvailability] = AvailabilityCloudOnly
		}
	})
}

// WalkCommands recursively calls fn on cmd and all its subcommands.
func WalkCommands(cmd *cobra.Command, fn func(*cobra.Command)) {
	fn(cmd)
	for _, sub := range cmd.Commands() {
		WalkCommands(sub, fn)
	}
}

// AnnotationRegistryPaths returns all command paths in the centralized
// annotation registry. Used by consistency tests to detect orphaned entries.
func AnnotationRegistryPaths() []string {
	paths := make([]string, 0, len(commandAnnotations))
	for p := range commandAnnotations {
		paths = append(paths, p)
	}
	return paths
}
