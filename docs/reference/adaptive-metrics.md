# Adaptive Metrics rules

Adaptive Metrics v2 rules do not store or return `managed_by`. The field remains in the `AggregationRule` schema as deprecated so older manifests and legacy recommendation data can still be read. A v2 rule write ignores its value, and a v2 read returns it empty.

Do not use `managed_by` to manage v2 rules. A manifest with a non-empty value can continue to show a diff after every pull because the v2 API cannot preserve that value. The schema marks the field deprecated, but this is guidance for schema-aware tools, not a runtime rejection or warning.
