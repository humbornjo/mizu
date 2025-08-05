package mizuotel

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Option configures the OpenTelemetry initialization.
type Option func(*config)

type config struct {
	serviceName    string
	serviceVersion string
	environment    string
	attrs          []attribute.KeyValue
	resource       *resource.Resource
	meterProvider  metric.MeterProvider
	tracerProvider trace.TracerProvider
}

// WithServiceName sets the service name for the OpenTelemetry
// resource.
func WithServiceName(name string) Option {
	return func(c *config) {
		c.serviceName = name
	}
}

// WithServiceVersion sets the service version for the
// OpenTelemetry resource.
func WithServiceVersion(version string) Option {
	return func(c *config) {
		c.serviceVersion = version
	}
}

// WithEnvironment sets the deployment environment for the
// OpenTelemetry resource.
func WithEnvironment(env string) Option {
	return func(c *config) {
		c.environment = env
	}
}

// WithAttributes adds custom attributes to the OpenTelemetry
// resource.
func WithAttributes(attrs ...attribute.KeyValue) Option {
	return func(c *config) {
		c.attrs = append(c.attrs, attrs...)
	}
}

// WithResource sets a custom OpenTelemetry resource, overriding
// the default resource creation.
func WithResource(res *resource.Resource) Option {
	return func(c *config) {
		c.resource = res
	}
}

// WithTracerProvider sets a custom tracer provider instead of
// creating a default one.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(c *config) {
		c.tracerProvider = tp
	}
}

// WithMeterProvider sets a custom meter provider instead of
// creating a default one.
func WithMeterProvider(mp metric.MeterProvider) Option {
	return func(c *config) {
		c.meterProvider = mp
	}
}

// Initialize sets up OpenTelemetry with the given options. It
// creates default tracer and meter providers with basic
// configuration, or uses custom providers if specified in the
// options.
func Initialize(opts ...Option) error {
	config := &config{
		serviceName:    "mizu-service",
		serviceVersion: "1.0.0",
		environment:    "development",
	}

	for _, opt := range opts {
		opt(config)
	}

	var res *resource.Resource
	var err error

	if config.resource != nil {
		res = config.resource
	} else {
		attrs := []attribute.KeyValue{
			semconv.ServiceName(config.serviceName),
			semconv.ServiceVersion(config.serviceVersion),
			semconv.DeploymentEnvironment(config.environment),
		}

		res, err = resource.Merge(
			resource.Default(),
			resource.NewWithAttributes(semconv.SchemaURL, attrs...),
		)
		if err != nil {
			return err
		}
	}

	// Merge additional attributes if provided
	if len(config.attrs) > 0 {
		additionalRes := resource.NewWithAttributes(semconv.SchemaURL, config.attrs...)
		res, err = resource.Merge(res, additionalRes)
		if err != nil {
			return err
		}
	}

	if config.tracerProvider != nil {
		otel.SetTracerProvider(config.tracerProvider)
	} else {
		tp := tracesdk.NewTracerProvider(tracesdk.WithResource(res))
		otel.SetTracerProvider(tp)
	}

	if config.meterProvider != nil {
		otel.SetMeterProvider(config.meterProvider)
	} else {
		mp := metricsdk.NewMeterProvider(metricsdk.WithResource(res))
		otel.SetMeterProvider(mp)
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.Baggage{},
		propagation.TraceContext{},
	))

	return nil
}

// Shutdown gracefully shuts down the OpenTelemetry providers.
// It attempts to shutdown both tracer and meter providers and
// returns the first error encountered.
func Shutdown(ctx context.Context) error {
	var errs []error

	if tp, ok := otel.GetTracerProvider().(*tracesdk.TracerProvider); ok {
		if err := tp.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	if mp, ok := otel.GetMeterProvider().(*metricsdk.MeterProvider); ok {
		if err := mp.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
