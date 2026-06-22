package obs

import (
	"context"
	"testing"
)

func TestInitTelemetryDisabled(t *testing.T) {
	tel, err := InitTelemetry(context.Background(), TelemetryConfig{
		DefaultServiceName: "test",
		OTELEnabled:        false,
		MetricsEnabled:     false,
	}, nil)
	if err != nil {
		t.Fatalf("InitTelemetry: %v", err)
	}
	if tel == nil {
		t.Fatal("expected telemetry handle")
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestDynamicMetricsRegistrationIsIdempotent(t *testing.T) {
	c1 := NewCounter("agentforge_test_dynamic_counter_total", "status")
	c2 := NewCounter("agentforge_test_dynamic_counter_total", "status")
	c1.Inc("ok")
	c2.Add(2, "ok")

	h1 := NewHistogram("agentforge_test_dynamic_histogram_seconds", "status")
	h2 := NewHistogram("agentforge_test_dynamic_histogram_seconds", "status")
	h1.Observe(0.1, "ok")
	h2.Observe(0.2, "ok")
}
