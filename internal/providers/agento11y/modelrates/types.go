// Package modelrates configures a tenant's own negotiated model prices.
//
// Every cost figure Agent Observability shows is otherwise computed from a
// public catalog, so a customer with a negotiated rate, a committed-use
// discount or a reseller agreement sees a number that is not theirs — and a
// customer running a model the catalog has never heard of sees no number at
// all.
//
// Rates are given in USD per million tokens, which is how providers publish a
// price and therefore what can be checked against a contract. The API converts
// to per-token on the way in and back again on the way out, so what this
// package sends and receives is always the reviewable number.
package modelrates

// Rate is one configured price as the API returns it.
//
// A nil rate and a zero cost the same: the pricer skips a nil bucket and
// multiplying by zero adds nothing. What differs is what each records — a zero
// says the contract prices that bucket at nothing, a nil says nothing about it
// — and that a row must set at least one rate, where a zero counts and a nil
// does not.
type Rate struct {
	Provider      string `json:"provider" yaml:"provider"`
	Model         string `json:"model" yaml:"model"`
	EffectiveFrom string `json:"effective_from" yaml:"effective_from"`

	InputUSDPerMillion      *float64 `json:"input_usd_per_million" yaml:"input_usd_per_million"`
	OutputUSDPerMillion     *float64 `json:"output_usd_per_million" yaml:"output_usd_per_million"`
	RequestUSD              *float64 `json:"request_usd" yaml:"request_usd"`
	CacheReadUSDPerMillion  *float64 `json:"cache_read_usd_per_million" yaml:"cache_read_usd_per_million"`
	CacheWriteUSDPerMillion *float64 `json:"cache_write_usd_per_million" yaml:"cache_write_usd_per_million"`

	LongContext *LongContextRate `json:"long_context,omitempty" yaml:"long_context,omitempty"`

	CreatedBy string `json:"created_by" yaml:"created_by"`
	UpdatedBy string `json:"updated_by" yaml:"updated_by"`
}

// LongContextRate is the tier billed above its threshold. A rate left unset
// falls back to the same row's base rate, which is coherent because both
// numbers come from one contract.
//
// So here, unlike the base rates, unset and zero are different prices: unset
// keeps charging the base rate above the threshold, zero makes that bucket
// free. A tier carrying only a threshold therefore changes nothing at all
// rather than pricing the request at nothing.
type LongContextRate struct {
	ThresholdInputTokens    int64    `json:"threshold_input_tokens" yaml:"threshold_input_tokens"`
	InputUSDPerMillion      *float64 `json:"input_usd_per_million,omitempty" yaml:"input_usd_per_million,omitempty"`
	OutputUSDPerMillion     *float64 `json:"output_usd_per_million,omitempty" yaml:"output_usd_per_million,omitempty"`
	CacheReadUSDPerMillion  *float64 `json:"cache_read_usd_per_million,omitempty" yaml:"cache_read_usd_per_million,omitempty"`
	CacheWriteUSDPerMillion *float64 `json:"cache_write_usd_per_million,omitempty" yaml:"cache_write_usd_per_million,omitempty"`
}

// RateWrite is the request body. A rate left nil is omitted rather than sent as
// zero: both charge nothing, but only a value sent counts toward the
// requirement that a row price something, and only a value sent records that
// the contract says so.
//
// It carries no effective_from: a write takes
// effect from the moment the server accepts it, and applying a rate to history
// is a separate job rather than a side effect of setting one. That is also why
// there is no update — every write records a new rate in force from now, and
// the previous one stays readable for the generations it priced.
type RateWrite struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`

	InputUSDPerMillion      *float64 `json:"input_usd_per_million,omitempty"`
	OutputUSDPerMillion     *float64 `json:"output_usd_per_million,omitempty"`
	RequestUSD              *float64 `json:"request_usd,omitempty"`
	CacheReadUSDPerMillion  *float64 `json:"cache_read_usd_per_million,omitempty"`
	CacheWriteUSDPerMillion *float64 `json:"cache_write_usd_per_million,omitempty"`

	LongContext *LongContextRate `json:"long_context,omitempty"`
}
