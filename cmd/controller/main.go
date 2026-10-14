package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/example/modeswitch/internal/controller"
	"github.com/example/modeswitch/internal/modesource"
)

var scheme = runtime.NewScheme()

func init() { utilruntime.Must(clientgoscheme.AddToScheme(scheme)) }

func parseModes(s string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, m := range strings.Split(s, ",") {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if m == "*" || strings.ContainsAny(m, "=,") {
			return nil, fmt.Errorf("invalid mode name %q", m)
		}
		if out[m] {
			return nil, fmt.Errorf("duplicate mode %q", m)
		}
		out[m] = true
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no modes given")
	}
	return out, nil
}

func main() {
	metricsAddr := flag.String("metrics-bind-address", ":8080", "Metrics endpoint address.")
	probeAddr := flag.String("health-probe-bind-address", ":8081", "Health probe address.")
	leaderElect := flag.Bool("leader-elect", true, "Enable leader election.")
	modeServer := flag.String("mode-server", "", "gRPC address of the mode server (host:port).")
	modeInsecure := flag.Bool("mode-server-insecure", false, "Plaintext gRPC (no TLS).")
	modeCA := flag.String("mode-server-ca", "", "CA bundle for the mode server (default: system roots).")
	modesFlag := flag.String("modes", "", "Comma-separated list of valid modes.")
	resync := flag.Duration("resync-interval", 5*time.Minute, "Periodic re-enqueue of all workloads (0 disables).")
	zopts := zap.Options{}
	zopts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zopts)))
	setupLog := ctrl.Log.WithName("setup")

	fail := func(err error, msg string) { setupLog.Error(err, msg); os.Exit(1) }

	known, err := parseModes(*modesFlag)
	if err != nil {
		fail(err, "invalid --modes")
	}
	if *modeServer == "" {
		fail(fmt.Errorf("--mode-server is required"), "invalid flags")
	}

	sel := labels.SelectorFromSet(labels.Set{controller.LabelEnabled: "true"})
	cfg := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: *metricsAddr},
		HealthProbeBindAddress: *probeAddr,
		LeaderElection:         *leaderElect,
		LeaderElectionID:       "modeswitch-controller." + controller.Domain,
		Cache: cache.Options{ByObject: map[client.Object]cache.ByObject{
			&appsv1.Deployment{}:  {Label: sel},
			&appsv1.StatefulSet{}: {Label: sel},
		}},
	})
	if err != nil {
		fail(err, "create manager")
	}

	clientset := kubernetes.NewForConfigOrDie(cfg)
	state := &controller.ModeState{}
	depEvents := make(chan event.GenericEvent, 1024)
	stsEvents := make(chan event.GenericEvent, 1024)

	for kind, ch := range map[controller.WorkloadKind]chan event.GenericEvent{
		controller.KindDeployment:  depEvents,
		controller.KindStatefulSet: stsEvents,
	} {
		r := &controller.WorkloadReconciler{
			Client:    mgr.GetClient(),
			Clientset: clientset,
			Kind:      kind,
			State:     state,
			Known:     known,
			Recorder:  mgr.GetEventRecorderFor(controller.FieldManager),
		}
		if err := r.SetupWithManager(mgr, ch); err != nil {
			fail(err, "setup controller "+string(kind))
		}
	}

	bc := &controller.Broadcaster{Client: mgr.GetClient(), DeploymentEvents: depEvents, StatefulSetEvents: stsEvents}

	var creds credentials.TransportCredentials
	switch {
	case *modeInsecure:
		creds = insecure.NewCredentials()
	case *modeCA != "":
		if creds, err = credentials.NewClientTLSFromFile(*modeCA, ""); err != nil {
			fail(err, "load --mode-server-ca")
		}
	default:
		creds = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	}

	src := &modesource.Source{
		Addr: *modeServer,
		DialOpts: []grpc.DialOption{
			grpc.WithTransportCredentials(creds),
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time: 30 * time.Second, Timeout: 10 * time.Second, PermitWithoutStream: true,
			}),
		},
		Known:    known,
		Set:      state.Set,
		Current:  state.Current,
		OnChange: func(ctx context.Context, _ string) error { return bc.EnqueueAll(ctx) },
		Resync:   *resync,
		Log:      ctrl.Log.WithName("modesource"),
	}
	if err := mgr.Add(src); err != nil {
		fail(err, "add mode source")
	}

	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		fail(err, "healthz")
	}
	if err := mgr.AddReadyzCheck("ping", healthz.Ping); err != nil {
		fail(err, "readyz")
	}

	setupLog.Info("starting", "modes", *modesFlag, "modeServer", *modeServer)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		fail(err, "manager exited")
	}
}
