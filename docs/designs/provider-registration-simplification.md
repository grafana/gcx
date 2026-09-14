# Provider registration simplification

Provider resources are declared through `adapter.Resource[T]`, with supported
operations inferred from client capability interfaces and common metadata derived
from the resource type. OnCall retains explicit wiring through the shared builder.

The design decision, constraints, alternatives, and migration scope are recorded
in [ADR-025](../adrs/declarative-provider-registration/001-declarative-resource-front-door.md).
