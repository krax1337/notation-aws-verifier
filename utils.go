package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

type secretInformer struct {
	informer cache.SharedIndexInformer
	lister   corev1listers.SecretLister
}

func NewSecretInformer(
	client kubernetes.Interface,
	namespace string,
	name string,
	resyncPeriod time.Duration,
) corev1informers.SecretInformer {
	indexers := cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}
	options := func(lo *metav1.ListOptions) {
		lo.FieldSelector = fields.OneTermEqualSelector(metav1.ObjectNameField, name).String()
	}
	informer := corev1informers.NewFilteredSecretInformer(
		client,
		namespace,
		resyncPeriod,
		indexers,
		options,
	)
	lister := corev1listers.NewSecretLister(informer.GetIndexer())
	return &secretInformer{informer, lister}
}

func (i *secretInformer) Informer() cache.SharedIndexInformer {
	return i.informer
}

func (i *secretInformer) Lister() corev1listers.SecretLister {
	return i.lister
}

func getEnvWithFallback(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// traceLevel is below debug; notation plugin and Kyverno V(2) logs use it.
const traceLevel = zapcore.Level(-2)

func parseLevel(s string) (zapcore.Level, error) {
	switch strings.ToLower(s) {
	case "trace":
		return traceLevel, nil
	case "debug":
		return zapcore.DebugLevel, nil
	case "info":
		return zapcore.InfoLevel, nil
	case "warn", "warning":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	default:
		return zapcore.InfoLevel, fmt.Errorf("invalid log level %q: must be one of trace, debug, info, warn, error", s)
	}
}

// newLogger builds the process logger. format "json" uses zap's production
// JSON encoder, "text" the human readable console encoder.
func newLogger(format, level string) (*zap.Logger, error) {
	lvl, err := parseLevel(level)
	if err != nil {
		return nil, err
	}

	var cfg zap.Config
	switch strings.ToLower(format) {
	case "json":
		cfg = zap.NewProductionConfig()
		cfg.EncoderConfig.TimeKey = "ts"
		cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	case "text":
		cfg = zap.NewDevelopmentConfig()
		// Development mode makes DPanic panic; never do that in a webhook backend.
		cfg.Development = false
	default:
		return nil, fmt.Errorf("invalid log format %q: must be one of text, json", format)
	}
	cfg.Level = zap.NewAtomicLevelAt(lvl)
	// Never drop admission logs.
	cfg.Sampling = nil

	return cfg.Build(zap.AddStacktrace(zapcore.DPanicLevel))
}

// splitList splits a comma-separated flag value, trimming blanks and dropping
// empty elements.
func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
