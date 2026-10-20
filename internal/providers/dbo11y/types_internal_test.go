package dbo11y

import "testing"

// Name has always been the service name where the collector sets one, falling
// back to the endpoint. Host is the endpoint regardless, so `instances get`
// resolves either spelling whichever way Name went.
func TestToInstance_NamePrefersServiceName(t *testing.T) {
	withService := apiInstance{
		Instance: "db.internal:5432",
		Labels:   map[string]string{"service_name": "payments", "service_namespace": "team-a", "deployment_environment": "prod"},
	}.toInstance()
	if withService.Name != "payments" {
		t.Errorf("name = %q, want the service name", withService.Name)
	}
	if withService.Host != "db.internal:5432" {
		t.Errorf("host = %q, want the endpoint", withService.Host)
	}
	if withService.Namespace != "team-a" || withService.Environment != "prod" {
		t.Errorf("namespace/environment = %q/%q, want them lifted out of labels", withService.Namespace, withService.Environment)
	}

	withoutService := apiInstance{Instance: "db.internal:5432"}.toInstance()
	if withoutService.Name != "db.internal:5432" {
		t.Errorf("name = %q, want the endpoint as fallback", withoutService.Name)
	}
	if withoutService.Namespace != "" || withoutService.Environment != "" {
		t.Errorf("namespace/environment should stay empty with no labels")
	}
}
