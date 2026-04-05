package agent

import "github.com/spf13/cobra"

// annotation is a compact representation of command annotations.
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
var commandAnnotations = map[string]annotation{
	// -----------------------------------------------------------------------
	// Core CLI commands (cmd/gcx/)
	// -----------------------------------------------------------------------

	// api
	"gcx api": {Cost: "large", Hint: "GET /api/datasources -o json"},

	// assistant
	// (already annotated inline)

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

	// dashboards
	// (already annotated inline)

	// datasources
	"gcx datasources get":   {Cost: "medium", Hint: "<uid> -o json"},
	"gcx datasources list":  {Cost: "small"},
	"gcx datasources query": {Cost: "large", Hint: "<datasource-uid> 'up' --since 1h -o json"},

	// dev
	"gcx dev generate":  {Cost: "small"},
	"gcx dev import":    {Cost: "medium", Hint: "dashboards -p ./dashboards"},
	"gcx dev scaffold":  {Cost: "small"},
	"gcx dev serve":     {Cost: "small"},
	"gcx dev lint new":  {Cost: "small"},
	"gcx dev lint rules": {Cost: "small"},
	"gcx dev lint run":  {Cost: "medium", Hint: "./dashboards -o compact"},
	"gcx dev lint test": {Cost: "medium", Hint: "./rules --run TestName"},

	// help-tree
	// (already annotated inline)

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
	"gcx setup status":                     {Cost: "small"},
	"gcx setup instrumentation apply":      {Cost: "small"},
	"gcx setup instrumentation discover":   {Cost: "medium", Hint: "--cluster <name> -o json"},
	"gcx setup instrumentation show":       {Cost: "medium", Hint: "<cluster> -o json"},
	"gcx setup instrumentation status":     {Cost: "small"},

	// -----------------------------------------------------------------------
	// Alert provider
	// -----------------------------------------------------------------------
	"gcx alert groups get":    {Cost: "small"},
	"gcx alert groups list":   {Cost: "small"},
	"gcx alert groups status": {Cost: "medium", Hint: "<name> -o json"},
	"gcx alert rules get":     {Cost: "small"},
	"gcx alert rules list":    {Cost: "medium", Hint: "--folder <uid> --group <name> -o json"},

	// -----------------------------------------------------------------------
	// App Observability provider
	// -----------------------------------------------------------------------
	"gcx appo11y overrides get":    {Cost: "small"},
	"gcx appo11y overrides update": {Cost: "small"},
	"gcx appo11y settings get":     {Cost: "small"},
	"gcx appo11y settings update":  {Cost: "small"},

	// -----------------------------------------------------------------------
	// Faro provider
	// -----------------------------------------------------------------------
	"gcx faro apps apply-sourcemap":  {Cost: "small"},
	"gcx faro apps create":           {Cost: "small"},
	"gcx faro apps delete":           {Cost: "small"},
	"gcx faro apps get":              {Cost: "small"},
	"gcx faro apps list":             {Cost: "small"},
	"gcx faro apps remove-sourcemap": {Cost: "small"},
	"gcx faro apps show-sourcemaps":  {Cost: "small"},
	"gcx faro apps update":           {Cost: "small"},

	// -----------------------------------------------------------------------
	// Fleet provider
	// -----------------------------------------------------------------------
	"gcx fleet collectors create": {Cost: "small"},
	"gcx fleet collectors delete": {Cost: "small"},
	"gcx fleet collectors get":    {Cost: "small"},
	"gcx fleet collectors list":   {Cost: "small"},
	"gcx fleet collectors update": {Cost: "small"},
	"gcx fleet pipelines create":  {Cost: "small"},
	"gcx fleet pipelines delete":  {Cost: "small"},
	"gcx fleet pipelines get":     {Cost: "small"},
	"gcx fleet pipelines list":    {Cost: "small"},
	"gcx fleet pipelines update":  {Cost: "small"},
	"gcx fleet tenant limits":     {Cost: "small"},

	// -----------------------------------------------------------------------
	// Incidents provider
	// -----------------------------------------------------------------------
	"gcx incidents activity add":   {Cost: "small"},
	"gcx incidents activity list":  {Cost: "small"},
	"gcx incidents close":          {Cost: "small"},
	"gcx incidents create":         {Cost: "small"},
	"gcx incidents get":            {Cost: "small"},
	"gcx incidents list":           {Cost: "small"},
	"gcx incidents open":           {Cost: "small"},
	"gcx incidents severities list": {Cost: "small"},

	// -----------------------------------------------------------------------
	// K6 provider
	// -----------------------------------------------------------------------
	"gcx k6 auth token":                        {Cost: "small"},
	"gcx k6 env-vars create":                   {Cost: "small"},
	"gcx k6 env-vars delete":                   {Cost: "small"},
	"gcx k6 env-vars list":                     {Cost: "small"},
	"gcx k6 env-vars update":                   {Cost: "small"},
	"gcx k6 load-tests create":                 {Cost: "small"},
	"gcx k6 load-tests delete":                 {Cost: "small"},
	"gcx k6 load-tests get":                    {Cost: "small"},
	"gcx k6 load-tests list":                   {Cost: "small"},
	"gcx k6 load-tests update":                 {Cost: "small"},
	"gcx k6 load-tests update-script":          {Cost: "small"},
	"gcx k6 load-zones allowed-load-zones list":   {Cost: "small"},
	"gcx k6 load-zones allowed-load-zones update": {Cost: "small"},
	"gcx k6 load-zones allowed-projects list":     {Cost: "small"},
	"gcx k6 load-zones allowed-projects update":   {Cost: "small"},
	"gcx k6 load-zones create":                 {Cost: "small"},
	"gcx k6 load-zones delete":                 {Cost: "small"},
	"gcx k6 load-zones list":                   {Cost: "small"},
	"gcx k6 projects create":                   {Cost: "small"},
	"gcx k6 projects delete":                   {Cost: "small"},
	"gcx k6 projects get":                      {Cost: "small"},
	"gcx k6 projects list":                     {Cost: "small"},
	"gcx k6 projects update":                   {Cost: "small"},
	"gcx k6 runs list":                         {Cost: "small"},
	"gcx k6 schedules create":                  {Cost: "small"},
	"gcx k6 schedules delete":                  {Cost: "small"},
	"gcx k6 schedules get":                     {Cost: "small"},
	"gcx k6 schedules list":                    {Cost: "small"},
	"gcx k6 schedules update":                  {Cost: "small"},
	"gcx k6 test-run emit":                     {Cost: "small"},
	"gcx k6 test-run runs list":                {Cost: "small"},
	"gcx k6 test-run status":                   {Cost: "small"},

	// -----------------------------------------------------------------------
	// Knowledge Graph provider
	// -----------------------------------------------------------------------
	"gcx kg datasets activate":    {Cost: "small"},
	"gcx kg datasets list":        {Cost: "small"},
	"gcx kg enable":               {Cost: "small"},
	"gcx kg entities list":        {Cost: "medium", Hint: "--type <type> --since 1h -o json"},
	"gcx kg entities show":        {Cost: "medium", Hint: "<Type--Name> --type <type> -o json"},
	"gcx kg entity-types list":    {Cost: "small"},
	"gcx kg graph-config":         {Cost: "small"},
	"gcx kg health":               {Cost: "medium", Hint: "--type <type> --since 1h -o json"},
	"gcx kg insights active":      {Cost: "medium", Hint: "--type <type> --severity critical -o json"},
	"gcx kg insights entity-metric": {Cost: "medium", Hint: "<Type--Name> --insight-id <id>"},
	"gcx kg insights example":     {Cost: "small"},
	"gcx kg insights graph":       {Cost: "medium", Hint: "<Type--Name> -o json"},
	"gcx kg insights query":       {Cost: "medium", Hint: "<Type--Name> -o json"},
	"gcx kg insights source-metrics": {Cost: "medium", Hint: "--insight-id <id> --since 1h"},
	"gcx kg insights summary":     {Cost: "medium", Hint: "<Type--Name> -o json"},
	"gcx kg inspect":              {Cost: "medium", Hint: "<Type--Name> -o json"},
	"gcx kg kpi-display create":   {Cost: "small"},
	"gcx kg model-rules create":   {Cost: "small"},
	"gcx kg open":                 {Cost: "small"},
	"gcx kg relabel-rules create": {Cost: "small"},
	"gcx kg rules create":         {Cost: "small"},
	"gcx kg rules delete":         {Cost: "small"},
	"gcx kg rules get":            {Cost: "small"},
	"gcx kg rules list":           {Cost: "small"},
	"gcx kg scopes list":          {Cost: "small"},
	"gcx kg search entities":      {Cost: "medium", Hint: "--type <type> -o json"},
	"gcx kg search example":       {Cost: "small"},
	"gcx kg search insights":      {Cost: "medium", Hint: "--type <type> --since 1h"},
	"gcx kg search sample":        {Cost: "small"},
	"gcx kg setup":                {Cost: "small"},
	"gcx kg status":               {Cost: "small"},
	"gcx kg suppressions create":  {Cost: "small"},
	"gcx kg vendors list":         {Cost: "small"},

	// -----------------------------------------------------------------------
	// Logs provider (adaptive)
	// -----------------------------------------------------------------------
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
	// logs query/labels/metrics/series already annotated in provider.go

	// -----------------------------------------------------------------------
	// Metrics provider (adaptive)
	// -----------------------------------------------------------------------
	"gcx metrics adaptive recommendations apply": {Cost: "small"},
	"gcx metrics adaptive recommendations show":  {Cost: "small"},
	"gcx metrics adaptive rules show":            {Cost: "small"},
	"gcx metrics adaptive rules sync":            {Cost: "medium", Hint: "-f rules.yaml --dry-run -o json"},
	// metrics query/labels/metadata already annotated in provider.go

	// -----------------------------------------------------------------------
	// OnCall provider
	// -----------------------------------------------------------------------
	"gcx oncall alert-groups acknowledge":  {Cost: "small"},
	"gcx oncall alert-groups delete":       {Cost: "small"},
	"gcx oncall alert-groups get":          {Cost: "small"},
	"gcx oncall alert-groups list":         {Cost: "small"},
	"gcx oncall alert-groups list-alerts":  {Cost: "small"},
	"gcx oncall alert-groups resolve":      {Cost: "small"},
	"gcx oncall alert-groups silence":      {Cost: "small"},
	"gcx oncall alert-groups unacknowledge": {Cost: "small"},
	"gcx oncall alert-groups unresolve":    {Cost: "small"},
	"gcx oncall alert-groups unsilence":    {Cost: "small"},
	"gcx oncall alerts get":                {Cost: "small"},
	"gcx oncall escalate":                  {Cost: "small"},
	"gcx oncall escalation-chains get":     {Cost: "small"},
	"gcx oncall escalation-chains list":    {Cost: "small"},
	"gcx oncall escalation-policies get":   {Cost: "small"},
	"gcx oncall escalation-policies list":  {Cost: "small"},
	"gcx oncall integrations get":          {Cost: "small"},
	"gcx oncall integrations list":         {Cost: "small"},
	"gcx oncall organizations get":         {Cost: "small"},
	"gcx oncall organizations list":        {Cost: "small"},
	"gcx oncall resolution-notes get":      {Cost: "small"},
	"gcx oncall resolution-notes list":     {Cost: "small"},
	"gcx oncall routes get":                {Cost: "small"},
	"gcx oncall routes list":               {Cost: "small"},
	"gcx oncall schedules final-shifts":    {Cost: "medium", Hint: "<schedule-id> --start 2024-01-01 --end 2024-01-31 -o json"},
	"gcx oncall schedules get":             {Cost: "small"},
	"gcx oncall schedules list":            {Cost: "small"},
	"gcx oncall shift-swaps get":           {Cost: "small"},
	"gcx oncall shift-swaps list":          {Cost: "small"},
	"gcx oncall shifts get":                {Cost: "small"},
	"gcx oncall shifts list":               {Cost: "small"},
	"gcx oncall slack-channels list":       {Cost: "small"},
	"gcx oncall teams get":                 {Cost: "small"},
	"gcx oncall teams list":                {Cost: "small"},
	"gcx oncall user-groups list":          {Cost: "small"},
	"gcx oncall users current":             {Cost: "small"},
	"gcx oncall users get":                 {Cost: "small"},
	"gcx oncall users list":                {Cost: "small"},
	"gcx oncall webhooks get":              {Cost: "small"},
	"gcx oncall webhooks list":             {Cost: "small"},

	// -----------------------------------------------------------------------
	// Profiles provider (adaptive)
	// -----------------------------------------------------------------------
	"gcx profiles adaptive": {Cost: "small"},
	// profiles query/labels/profile-types/metrics already annotated in provider.go

	// -----------------------------------------------------------------------
	// Sigil provider
	// -----------------------------------------------------------------------
	"gcx sigil conversations search": {Cost: "medium", Hint: "--from 2024-01-01 --to 2024-01-31 -o json"},
	"gcx sigil conversations show":   {Cost: "medium", Hint: "--limit 10 -o json"},

	// -----------------------------------------------------------------------
	// SLO provider
	// -----------------------------------------------------------------------
	"gcx slo definitions delete":   {Cost: "small"},
	"gcx slo definitions get":      {Cost: "small"},
	"gcx slo definitions list":     {Cost: "small"},
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
	"gcx synth checks create":   {Cost: "small"},
	"gcx synth checks delete":   {Cost: "small"},
	"gcx synth checks get":      {Cost: "small"},
	"gcx synth checks list":     {Cost: "small"},
	"gcx synth checks status":   {Cost: "medium", Hint: "--job <name> -o json"},
	"gcx synth checks timeline": {Cost: "medium", Hint: "<id> --since 1h -o json"},
	"gcx synth checks update":   {Cost: "small"},
	"gcx synth probes list":     {Cost: "small"},

	// -----------------------------------------------------------------------
	// Traces provider (adaptive)
	// -----------------------------------------------------------------------
	"gcx traces adaptive policies create":       {Cost: "small"},
	"gcx traces adaptive policies delete":       {Cost: "small"},
	"gcx traces adaptive policies get":          {Cost: "small"},
	"gcx traces adaptive policies list":         {Cost: "small"},
	"gcx traces adaptive policies update":       {Cost: "small"},
	"gcx traces adaptive recommendations apply":    {Cost: "small"},
	"gcx traces adaptive recommendations dismiss":  {Cost: "small"},
	"gcx traces adaptive recommendations show":     {Cost: "small"},
	// traces query/get/labels/metrics already annotated in provider.go
}

// ApplyAnnotations walks the command tree and applies agent annotations from
// the centralized registry. Existing annotations on a command are preserved;
// registry entries only fill in missing keys. Call this after the full command
// tree is assembled.
func ApplyAnnotations(root *cobra.Command) {
	walkCmd(root, func(cmd *cobra.Command) {
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

func walkCmd(cmd *cobra.Command, fn func(*cobra.Command)) {
	fn(cmd)
	for _, sub := range cmd.Commands() {
		walkCmd(sub, fn)
	}
}
