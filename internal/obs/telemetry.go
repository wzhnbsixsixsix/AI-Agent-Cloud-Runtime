package obs

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type TelemetryConfig struct {
	ServiceName        string
	DefaultServiceName string
	OTELEnabled        bool
	OTLPEndpoint       string
	MetricsEnabled     bool
	MetricsAddr        string
	MetricsPath        string
}

type Telemetry struct {
	tracerProvider *sdktrace.TracerProvider
	metricsServer  *http.Server
}

func InitTelemetry(ctx context.Context, cfg TelemetryConfig, log *slog.Logger) (*Telemetry, error) {
	service := cfg.ServiceName
	if service == "" {
		service = cfg.DefaultServiceName
	}
	if service == "" {
		service = "agentforge"
	}
	SetServiceName(service)
	t := &Telemetry{}
	var initErr error
	if cfg.OTELEnabled {
		tp, err := newTracerProvider(ctx, service, cfg.OTLPEndpoint)
		if err != nil {
			initErr = errors.Join(initErr, err)
		} else {
			t.tracerProvider = tp
			otel.SetTracerProvider(tp)
			otel.SetTextMapPropagator(propagation.TraceContext{})
		}
	}
	if cfg.MetricsEnabled {
		addr := cfg.MetricsAddr
		if addr == "" {
			addr = ":9090"
		}
		path := cfg.MetricsPath
		if path == "" {
			path = "/metrics"
		}
		mux := http.NewServeMux()
		mux.Handle(path, MetricsHandler())
		srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		t.metricsServer = srv
		go func() {
			if log != nil {
				log.Info("metrics serving", "addr", addr, "path", path)
			}
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				if log != nil {
					log.Warn("metrics serve failed", "err", err)
				}
			}
		}()
	}
	return t, initErr
}

func newTracerProvider(ctx context.Context, service, endpoint string) (*sdktrace.TracerProvider, error) {
	if endpoint == "" {
		endpoint = "otel-collector:4317"
	}
	exp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes("",
		attribute.String("service.name", service),
		attribute.String("service.namespace", "agentforge"),
	))
	if err != nil {
		return nil, err
	}
	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	), nil
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}
	var err error
	if t.metricsServer != nil {
		err = errors.Join(err, t.metricsServer.Shutdown(ctx))
	}
	if t.tracerProvider != nil {
		err = errors.Join(err, t.tracerProvider.Shutdown(ctx))
	}
	return err
}

func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	base := []attribute.KeyValue{
		attribute.String("agentforge.service", ServiceName()),
	}
	if runID := RunIDFromContext(ctx); runID != "" {
		base = append(base, attribute.String("agentforge.run_id", runID))
	}
	if traceID := TraceIDFromContext(ctx); traceID != "" {
		base = append(base, attribute.String("agentforge.trace_id", traceID))
	}
	base = append(base, attrs...)
	return otel.Tracer("github.com/wzhnbsixsixsix/agentforge").Start(ctx, name, trace.WithAttributes(base...))
}

func EndSpan(span trace.Span, err error) {
	if span == nil {
		return
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}

func Attr(key, value string) attribute.KeyValue {
	return attribute.String(key, value)
}

func IntAttr(key string, value int) attribute.KeyValue {
	return attribute.Int(key, value)
}

func BoolAttr(key string, value bool) attribute.KeyValue {
	return attribute.Bool(key, value)
}
