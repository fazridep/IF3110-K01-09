// Package obs berisi logging JSON dan tracing OpenTelemetry yang dipakai bareng.
package obs

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	ExporterConsole = "console"
	ExporterNone    = "none"
)

type Config struct {
	Service       string
	Level         slog.Level
	TraceExporter string
	LogOut        io.Writer
	TraceOut      io.Writer
}

var propagator = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})

func ParseLevel(s string) (slog.Level, error) {
	var l slog.Level
	if s == "" {
		return slog.LevelInfo, nil
	}
	if err := l.UnmarshalText([]byte(strings.ToUpper(s))); err != nil {
		return 0, fmt.Errorf("obs: level log %q tidak dikenal", s)
	}
	return l, nil
}

// Setup masang logger default dan tracer provider global.
func Setup(ctx context.Context, cfg Config) error {
	if cfg.Service == "" {
		cfg.Service = "db-platform"
	}
	if cfg.LogOut == nil {
		cfg.LogOut = os.Stdout
	}
	if cfg.TraceOut == nil {
		cfg.TraceOut = os.Stderr
	}
	if cfg.TraceExporter == "" {
		cfg.TraceExporter = ExporterConsole
	}
	if cfg.TraceExporter != ExporterConsole && cfg.TraceExporter != ExporterNone {
		return fmt.Errorf("obs: exporter trace %q tidak dikenal (pakai console atau none)", cfg.TraceExporter)
	}

	slog.SetDefault(NewLogger(cfg.LogOut, cfg.Level).With("service", cfg.Service))

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", cfg.Service))),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	}
	if cfg.TraceExporter == ExporterConsole {
		exp, err := stdouttrace.New(stdouttrace.WithWriter(cfg.TraceOut))
		if err != nil {
			return fmt.Errorf("obs: buat exporter console: %w", err)
		}
		opts = append(opts, sdktrace.WithSyncer(exp))
	}
	otel.SetTracerProvider(sdktrace.NewTracerProvider(opts...))
	otel.SetTextMapPropagator(propagator)
	return nil
}
