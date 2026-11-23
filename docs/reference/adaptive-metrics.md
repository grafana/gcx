# Adaptive Metrics rules

Adaptive Metrics v2 rules do not store or return `managed_by`. The field remains in the `AggregationRule` schema as deprecated so older manifests and legacy recommendation data can still be read. A v2 rule write ignores its value, and a v2 read returns it empty.

Do not use `managed_by` to manage v2 rules. A manifest with a non-empty value can continue to show a diff after every pull because the v2 API cannot preserve that value. The schema marks the field deprecated, but this is guidance for schema-aware tools, not a runtime rejection or warning.

## Deleting rules from manifests

When you pass an `AggregationRule` manifest to `gcx resources delete -p <path>`, gcx counts the deletion as successful even if the rule is already absent. The dedicated `gcx metrics adaptive rules delete <metric>` command still reports an error when the rule does not exist.

## Recommendation previews

`gcx metrics adaptive recommendations apply --all --dry-run` summarizes recommendations in the analyzed metrics catalog. It counts `keep` verdicts as skipped and leaves out `none` verdicts because there is no recommendation to apply. If you name a metric explicitly, a `none` verdict counts as skipped because the metric was part of your request.
