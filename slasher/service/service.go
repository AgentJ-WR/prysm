// Package service defines the service used to retrieve slashings proofs and
// feed attestations and block headers into the slasher db.
package service

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path"
	"sync"
	"syscall"

	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	grpc_opentracing "github.com/grpc-ecosystem/go-grpc-middleware/tracing/opentracing"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/pkg/errors"
	eth "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	slashpb "github.com/prysmaticlabs/prysm/proto/slashing"
	"github.com/prysmaticlabs/prysm/shared/cmd"
	"github.com/prysmaticlabs/prysm/shared/debug"
	"github.com/prysmaticlabs/prysm/shared/version"
	"github.com/prysmaticlabs/prysm/slasher/db"
	"github.com/prysmaticlabs/prysm/slasher/flags"
	"github.com/prysmaticlabs/prysm/slasher/rpc"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"go.opencensus.io/plugin/ocgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
)

var log logrus.FieldLogger

const slasherDBName = "slasherdata"

func init() {
	log = logrus.WithField("prefix", "slasherRPC")
}

// Service defining an RPC server for the slasher service.
type Service struct {
	slasherDb       *db.Store
	grpcServer      *grpc.Server
	slasher         *rpc.Server
	port            int
	withCert        string
	withKey         string
	listener        net.Listener
	credentialError error
	failStatus      error
	ctx             *cli.Context
	lock            sync.RWMutex
	stop            chan struct{} // Channel to wait for termination notifications.
	context         context.Context
	beaconConn      *grpc.ClientConn
	beaconProvider  string
	beaconCert      string
	beaconClient    eth.BeaconChainClient
	started         bool
}

// Config options for the slasher server.
type Config struct {
	Port           int
	CertFlag       string
	KeyFlag        string
	SlasherDb      *db.Store
	BeaconProvider string
	BeaconCert     string
}

// NewRPCService creates a new instance of a struct implementing the SlasherService
// interface.
func NewRPCService(cfg *Config, ctx *cli.Context) (*Service, error) {
	s := &Service{
		slasherDb:      cfg.SlasherDb,
		port:           cfg.Port,
		withCert:       cfg.CertFlag,
		withKey:        cfg.KeyFlag,
		ctx:            ctx,
		stop:           make(chan struct{}),
		beaconProvider: cfg.BeaconProvider,
		beaconCert:     cfg.BeaconCert,
	}
	if err := s.startDB(s.ctx); err != nil {
		return nil, err
	}
	s.slasher = &rpc.Server{
		SlasherDB: s.slasherDb,
	}
	return s, nil
}

// Start the gRPC server.
func (s *Service) Start() {
	s.lock.Lock()
	log.WithFields(logrus.Fields{
		"version": version.GetVersion(),
	}).Info("Starting hash slinging slasher node")
	s.context = context.Background()
	s.startSlasher()
	if s.beaconClient == nil {
		if err := s.startBeaconClient(); err != nil {
			log.WithError(err).Errorf("failed to start beacon client")
			s.failStatus = err
			return
		}
	}
	stop := s.stop
	err := s.slasherOldAttestationFeeder()
	if err != nil {
		err = errors.Wrap(err, "couldn't start attestation feeder from archive endpoint. please use "+
			"--beacon-rpc-provider flag value if you are not running a beacon chain service with "+
			"--archive flag on the local machine.")
		log.WithError(err)
		s.failStatus = err
		return
	}
	go s.attestationFeeder()
	go s.finalisedChangeUpdater()
	s.lock.Unlock()

	go func() {
		sigc := make(chan os.Signal, 1)
		signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sigc)
		<-sigc
		log.Info("Got interrupt, shutting down...")
		debug.Exit(s.ctx) // Ensure trace and CPU profile data are flushed.
		go s.Close()
		for i := 10; i > 0; i-- {
			<-sigc
			if i > 1 {
				log.Info("Already shutting down, interrupt more to panic", "times", i-1)
			}
		}
		panic("Panic closing the hash slinging slasher node")
	}()
	s.started = true
	// Wait for stop channel to be closed.
	<-stop

}

func (s *Service) startSlasher() {
	log.Info("Starting service on port: ", s.port)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		log.Errorf("Could not listen to port in Start() :%d: %v", s.port, err)
	}
	s.listener = lis
	log.WithField("port", s.port).Info("Listening on port")

	opts := []grpc.ServerOption{
		grpc.StatsHandler(&ocgrpc.ServerHandler{}),
		grpc.StreamInterceptor(middleware.ChainStreamServer(
			recovery.StreamServerInterceptor(),
			grpc_prometheus.StreamServerInterceptor,
		)),
		grpc.UnaryInterceptor(middleware.ChainUnaryServer(
			recovery.UnaryServerInterceptor(),
			grpc_prometheus.UnaryServerInterceptor,
		)),
	}
	// TODO(#791): Utilize a certificate for secure connections
	// between beacon nodes and validator clients.
	if s.withCert != "" && s.withKey != "" {
		creds, err := credentials.NewServerTLSFromFile(s.withCert, s.withKey)
		if err != nil {
			log.Errorf("Could not load TLS keys: %s", err)
			s.credentialError = err
		}
		opts = append(opts, grpc.Creds(creds))
	} else {
		log.Warn("You are using an insecure gRPC connection! Provide a certificate and key to connect securely")
	}
	s.grpcServer = grpc.NewServer(opts...)
	slasherServer := rpc.Server{
		SlasherDB: s.slasherDb,
	}
	if s.ctx.GlobalBool(flags.RebuildSpanMapsFlag.Name) {
		s.loadSpanMaps(err, slasherServer)
	}
	slashpb.RegisterSlasherServer(s.grpcServer, &slasherServer)

	// Register reflection service on gRPC server.
	reflection.Register(s.grpcServer)

	go func() {
		if s.listener != nil {
			if err := s.grpcServer.Serve(s.listener); err != nil {
				log.Errorf("Could not serve gRPC: %v", err)
			}
		}
	}()
}

func (s *Service) loadSpanMaps(err error, slasherServer rpc.Server) {
	lt, err := slasherServer.SlasherDB.LatestIndexedAttestationsTargetEpoch()
	if err != nil {
		log.Errorf("Could not extract latest target epoch from indexed attestations store: %v", err)
	}
	for i := uint64(0); i < lt; i++ {
		ias, err := slasherServer.SlasherDB.IndexedAttestations(i)
		if err != nil {
			log.Errorf("Got error while trying to retrieve indexed attestations from db: %v", err)
		}
		for _, ia := range ias {
			slasherServer.UpdateSpanMaps(s.context, ia)
		}
		log.Infof("Update span maps for epoch: %d", i)
	}
}

func (s *Service) startBeaconClient() error {
	var dialOpt grpc.DialOption

	if s.beaconCert != "" {
		creds, err := credentials.NewClientTLSFromFile(s.beaconCert, "")
		if err != nil {
			log.Errorf("Could not get valid credentials: %v", err)
		}
		dialOpt = grpc.WithTransportCredentials(creds)
	} else {
		dialOpt = grpc.WithInsecure()
		log.Warn("You are using an insecure gRPC connection to beacon chain! Please provide a certificate and key to use a secure connection.")
	}
	beaconOpts := []grpc.DialOption{
		dialOpt,
		grpc.WithStatsHandler(&ocgrpc.ClientHandler{}),
		grpc.WithStreamInterceptor(middleware.ChainStreamClient(
			grpc_opentracing.StreamClientInterceptor(),
			grpc_prometheus.StreamClientInterceptor,
		)),
		grpc.WithUnaryInterceptor(middleware.ChainUnaryClient(
			grpc_opentracing.UnaryClientInterceptor(),
			grpc_prometheus.UnaryClientInterceptor,
		)),
	}
	conn, err := grpc.DialContext(s.context, s.beaconProvider, beaconOpts...)
	if err != nil {
		return fmt.Errorf("could not dial endpoint: %s, %v", s.beaconProvider, err)
	}
	log.Info("Successfully started gRPC connection")
	s.beaconConn = conn
	s.beaconClient = eth.NewBeaconChainClient(s.beaconConn)
	return nil
}

// Stop the service.
func (s *Service) Stop() error {
	log.Info("Stopping service")
	if s.slasherDb != nil {
		s.slasherDb.Close()
	}
	if s.listener != nil {
		s.grpcServer.GracefulStop()
		log.Debug("Initiated graceful stop of gRPC server")
	}
	return nil
}

// Close handles graceful shutdown of the system.
func (s *Service) Close() {
	s.lock.Lock()
	defer s.lock.Unlock()
	log.Info("Stopping hash slinging slasher")
	err := s.Stop()
	if err != nil {
		log.Panicf("Could not stop the slasher service: %v", err)
	}
	if err := s.slasherDb.SaveCachedSpansMaps(); err != nil {
		log.Fatal("Didn't save span map cache to db. if span cache is enabled please restart with --%s", flags.RebuildSpanMapsFlag.Name)
	}
	if err := s.slasherDb.Close(); err != nil {
		log.Errorf("Failed to close slasher database: %v", err)
	}
	s.context.Done()
	close(s.stop)
}

// Status returns nil, credentialError or fail status.
func (s *Service) Status() (bool, error) {
	if s.credentialError != nil {
		return false, s.credentialError
	}
	if s.failStatus != nil {
		return false, s.failStatus
	}
	return s.started, nil

}

func (s *Service) startDB(ctx *cli.Context) error {
	baseDir := ctx.GlobalString(cmd.DataDirFlag.Name)
	dbPath := path.Join(baseDir, slasherDBName)
	cfg := &db.Config{SpanCacheEnabled: ctx.GlobalBool(flags.UseSpanCacheFlag.Name)}
	d, err := db.NewDB(dbPath, cfg)
	if err != nil {
		return err
	}
	if s.ctx.GlobalBool(cmd.ClearDB.Name) {
		if err := d.ClearDB(); err != nil {
			return err
		}
		d, err = db.NewDB(dbPath, cfg)
		if err != nil {
			return err
		}
	}

	log.WithField("path", dbPath).Info("Checking db")
	s.slasherDb = d
	return nil
}
