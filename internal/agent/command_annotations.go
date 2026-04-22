package agent

import (
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
	"gcx assistant investigations approvals": {Cost: "medium", Hint: "<id> -o json"},
	"gcx assistant investigations cancel":    {Cost: "small"},
	"gcx assistant investigations create":    {Cost: "small", Hint: "Use for deep cross-signal root cause analysis. Dispatches specialist agents for metrics, logs, traces, and profiles in parallel — more efficient than chaining individual gcx query commands. Example: --title=\"Checkout latency spike after deploy\""},
	"gcx assistant investigations document":  {Cost: "medium", Hint: "<investigation-id> <document-id> -o json"},
	"gcx assistant investigations get":       {Cost: "medium", Hint: "<id> -o json"},
	"gcx assistant investigations list":      {Cost: "small"},
	"gcx assistant investigations report":    {Cost: "medium", Hint: "<id> -o json"},
	"gcx assistant investigations timeline":  {Cost: "medium", Hint: "<id> -o json"},
	"gcx assistant investigations todos":     {Cost: "medium", Hint: "<id> -o json"},

	// auth
	"gcx auth login": {Cost: "small"},

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
	"gcx resources examples": {Cost: "small"},
	"gcx resources get":      {Cost: "large", Hint: "dashboards/my-uid -o json"},
	"gcx resources pull":     {Cost: "large", Hint: "dashboards -p ./dashboards"},
	"gcx resources push":     {Cost: "medium", Hint: "-p ./dashboards --dry-run"},
	"gcx resources schemas":  {Cost: "small"},
	"gcx resources validate": {Cost: "medium", Hint: "-p ./dashboards"},

	// setup
	"gcx setup status":                   {Cost: "small"},
	"gcx setup instrumentation apply":    {Cost: "small"},
	"gcx setup instrumentation discover": {Cost: "medium", Hint: "--cluster <name> -o json"},
	"gcx setup instrumentation show":     {Cost: "medium", Hint: "<cluster> -o json"},
	"gcx setup instrumentation status":   {Cost: "small"},

	// skills
	"gcx skills install":   {Cost: "small"},
	"gcx skills list":      {Cost: "small"},
	"gcx skills uninstall": {Cost: "small"},

	// -----------------------------------------------------------------------
	// Alert provider
	// -----------------------------------------------------------------------
	"gcx alert groups get":     {Cost: "small"},
	"gcx alert groups list":    {Cost: "small"},
	"gcx alert groups status":  {Cost: "medium", Hint: "<name> -o json"},
	"gcx alert instances list": {Cost: "large", Hint: "--state firing --group <name> -o json"},
	"gcx alert rules get":      {Cost: "small"},
	"gcx alert rules list":     {Cost: "medium", Hint: "--folder <uid> --group <name> -o json"},

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
	"gcx k6 load-tests create":                    {Cost: "small", Hint: "-f <manifest.yaml>"},
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
	"gcx k6 test-run emit":                        {Cost: "small", Hint: "[test-name] --project-id <id> [--apply]"},
	"gcx k6 test-run runs list":                   {Cost: "small"},
	"gcx k6 test-run status":                      {Cost: "small"},

	// -----------------------------------------------------------------------
	// Knowledge Graph provider
	// -----------------------------------------------------------------------
	"gcx kg entities list":           {Cost: "medium", Hint: "--type <type> --since 1h -o json"},
	"gcx kg entities show":           {Cost: "medium", Hint: "<Type--Name> --type <type> -o json"},
	"gcx kg health":                  {Cost: "medium", Hint: "--type <type> --since 1h -o json"},
	"gcx kg insights active":         {Cost: "medium", Hint: "--type <type> --severity critical -o json"},
	"gcx kg insights entity-metric":  {Cost: "medium", Hint: "<Type--Name> --insight-id <id>"},
	"gcx kg insights example":        {Cost: "small"},
	"gcx kg insights graph":          {Cost: "medium", Hint: "<Type--Name> -o json"},
	"gcx kg insights query":          {Cost: "medium", Hint: "<Type--Name> -o json"},
	"gcx kg insights source-metrics": {Cost: "medium", Hint: "--insight-id <id> --since 1h"},
	"gcx kg insights summary":        {Cost: "medium", Hint: "<Type--Name> -o json"},
	"gcx kg inspect":                 {Cost: "medium", Hint: "<Type--Name> -o json"},
	"gcx kg model-rules create":      {Cost: "small"},
	"gcx kg open":                    {Cost: "small"},
	"gcx kg relabel-rules create":    {Cost: "small"},
	"gcx kg rules create":            {Cost: "small"},
	"gcx kg rules delete":            {Cost: "small"},
	"gcx kg rules get":               {Cost: "small"},
	"gcx kg rules list":              {Cost: "small"},
	"gcx kg scopes list":             {Cost: "small"},
	"gcx kg search example":          {Cost: "small"},
	"gcx kg search insights":         {Cost: "medium", Hint: "--type <type> --since 1h"},
	"gcx kg search sample":           {Cost: "small"},
	"gcx kg status":                  {Cost: "small"},
	"gcx kg suppressions create":     {Cost: "small"},

	// -----------------------------------------------------------------------
	// Logs provider
	// -----------------------------------------------------------------------
	"gcx logs labels":  {Cost: "small"},
	"gcx logs metrics": {Cost: "large", Hint: "'rate({job=\"myapp\"}[5m])' --since 1h -o json"},
	"gcx logs query":   {Cost: "large", Hint: "'{job=\"myapp\"}' --since 1h --limit 100 -o json"},
	"gcx logs series":  {Cost: "medium", Hint: "--match '{job=\"myapp\"}' -o json"},

	// Logs adaptive
	"gcx logs adaptive drop-rules create": {Cost: "small"},
	"gcx logs adaptive drop-rules delete": {Cost: "small"},
	"gcx logs adaptive drop-rules get":    {Cost: "small"},
	"gcx logs adaptive drop-rules list":   {Cost: "small"},
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
	"gcx metrics query":    {Cost: "large", Hint: "'up' --since 1h -o json"},

	// Metrics adaptive
	"gcx metrics adaptive recommendations apply": {Cost: "small"},
	"gcx metrics adaptive recommendations diff":  {Cost: "medium", Hint: "<metric> -o json"},
	"gcx metrics adaptive recommendations show":  {Cost: "small"},
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
	"gcx irm oncall alert-groups acknowledge":   {Cost: "small"},
	"gcx irm oncall alert-groups delete":        {Cost: "small"},
	"gcx irm oncall alert-groups get":           {Cost: "small"},
	"gcx irm oncall alert-groups list":          {Cost: "small"},
	"gcx irm oncall alert-groups list-alerts":   {Cost: "small"},
	"gcx irm oncall alert-groups resolve":       {Cost: "small"},
	"gcx irm oncall alert-groups silence":       {Cost: "small"},
	"gcx irm oncall alert-groups unacknowledge": {Cost: "small"},
	"gcx irm oncall alert-groups unresolve":     {Cost: "small"},
	"gcx irm oncall alert-groups unsilence":     {Cost: "small"},
	"gcx irm oncall alerts get":                 {Cost: "small"},
	"gcx irm oncall escalate":                   {Cost: "small", Hint: "--title \"title\" --user-ids id1,id2"},
	"gcx irm oncall escalation-chains get":      {Cost: "small"},
	"gcx irm oncall escalation-chains list":     {Cost: "small"},
	"gcx irm oncall escalation-policies get":    {Cost: "small"},
	"gcx irm oncall escalation-policies list":   {Cost: "small"},
	"gcx irm oncall integrations get":           {Cost: "small"},
	"gcx irm oncall integrations list":          {Cost: "small"},
	"gcx irm oncall organizations get":          {Cost: "small"},
	"gcx irm oncall resolution-notes get":       {Cost: "small"},
	"gcx irm oncall resolution-notes list":      {Cost: "small"},
	"gcx irm oncall routes get":                 {Cost: "small"},
	"gcx irm oncall routes list":                {Cost: "small"},
	"gcx irm oncall schedules final-shifts":     {Cost: "medium", Hint: "<schedule-id> --start 2024-01-01 --end 2024-01-31 -o json"},
	"gcx irm oncall schedules get":              {Cost: "small"},
	"gcx irm oncall schedules list":             {Cost: "small"},
	"gcx irm oncall shift-swaps get":            {Cost: "small"},
	"gcx irm oncall shift-swaps list":           {Cost: "small"},
	"gcx irm oncall shifts get":                 {Cost: "small"},
	"gcx irm oncall shifts list":                {Cost: "small"},
	"gcx irm oncall slack-channels list":        {Cost: "small"},
	"gcx irm oncall teams get":                  {Cost: "small"},
	"gcx irm oncall teams list":                 {Cost: "small"},
	"gcx irm oncall user-groups list":           {Cost: "small"},
	"gcx irm oncall users current":              {Cost: "small"},
	"gcx irm oncall users get":                  {Cost: "small"},
	"gcx irm oncall users list":                 {Cost: "small"},
	"gcx irm oncall webhooks get":               {Cost: "small"},
	"gcx irm oncall webhooks list":              {Cost: "small"},

	// -----------------------------------------------------------------------
	// Profiles provider
	// -----------------------------------------------------------------------
	"gcx profiles adaptive":      {Cost: "small"},
	"gcx profiles labels":        {Cost: "small"},
	"gcx profiles metrics":       {Cost: "large", Hint: "'{service_name=\"frontend\"}' --profile-type cpu --since 1h -o json"},
	"gcx profiles profile-types": {Cost: "small"},
	"gcx profiles query":         {Cost: "large", Hint: "'{service_name=\"frontend\"}' --profile-type cpu --since 1h -o json"},

	// -----------------------------------------------------------------------
	// AI Observability provider
	// -----------------------------------------------------------------------
	"gcx aio11y agents get":      {Cost: "small"},
	"gcx aio11y agents list":     {Cost: "small"},
	"gcx aio11y agents versions": {Cost: "small"},

	"gcx aio11y conversations get":    {Cost: "medium", Hint: "<conversation-id> -o json"},
	"gcx aio11y conversations list":   {Cost: "small"},
	"gcx aio11y conversations search": {Cost: "medium", Hint: "--from 2024-01-01 --to 2024-01-31 -o json"},

	"gcx aio11y evaluators create": {Cost: "small"},
	"gcx aio11y evaluators delete": {Cost: "small"},
	"gcx aio11y evaluators get":    {Cost: "small"},
	"gcx aio11y evaluators list":   {Cost: "small"},
	"gcx aio11y evaluators test":   {Cost: "medium", Hint: "<evaluator-id> -o json"},

	"gcx aio11y generations get": {Cost: "medium", Hint: "<generation-id> -o json"},

	"gcx aio11y judge models":    {Cost: "small"},
	"gcx aio11y judge providers": {Cost: "small"},

	"gcx aio11y rules create": {Cost: "small"},
	"gcx aio11y rules delete": {Cost: "small"},
	"gcx aio11y rules get":    {Cost: "small"},
	"gcx aio11y rules list":   {Cost: "small"},
	"gcx aio11y rules update": {Cost: "small"},

	"gcx aio11y scores list": {Cost: "small"},

	"gcx aio11y templates get":      {Cost: "small"},
	"gcx aio11y templates list":     {Cost: "small"},
	"gcx aio11y templates versions": {Cost: "small"},

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
	"gcx synth checks create":      {Cost: "small"},
	"gcx synth checks delete":      {Cost: "small"},
	"gcx synth checks get":         {Cost: "small"},
	"gcx synth checks list":        {Cost: "small"},
	"gcx synth checks status":      {Cost: "medium", Hint: "--job <name> -o json"},
	"gcx synth checks timeline":    {Cost: "medium", Hint: "<id> --since 1h -o json"},
	"gcx synth checks update":      {Cost: "small"},
	"gcx synth probes create":      {Cost: "small"},
	"gcx synth probes delete":      {Cost: "small"},
	"gcx synth probes deploy":      {Cost: "small"},
	"gcx synth probes list":        {Cost: "small"},
	"gcx synth probes token-reset": {Cost: "small"},

	// -----------------------------------------------------------------------
	// Traces provider
	// -----------------------------------------------------------------------
	"gcx traces get":     {Cost: "large", Hint: "<trace-id> --llm -o json"},
	"gcx traces labels":  {Cost: "small"},
	"gcx traces metrics": {Cost: "large", Hint: "'rate({ span.http.status_code >= 500 }[5m])' --since 1h -o json"},
	"gcx traces query":   {Cost: "large", Hint: "'{ span.http.status_code >= 500 }' --since 1h --limit 20 -o json"},

	// Traces adaptive
	"gcx traces adaptive policies create":         {Cost: "small"},
	"gcx traces adaptive policies delete":         {Cost: "small"},
	"gcx traces adaptive policies get":            {Cost: "small"},
	"gcx traces adaptive policies list":           {Cost: "small"},
	"gcx traces adaptive policies update":         {Cost: "small"},
	"gcx traces adaptive recommendations apply":   {Cost: "small"},
	"gcx traces adaptive recommendations dismiss": {Cost: "small"},
	"gcx traces adaptive recommendations show":    {Cost: "small"},
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
