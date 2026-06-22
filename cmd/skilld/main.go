package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/config"
	"github.com/wzhnbsixsixsix/agentforge/internal/discovery"
	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	"github.com/wzhnbsixsixsix/agentforge/internal/skill"
	pb "github.com/wzhnbsixsixsix/agentforge/pkg/proto/gen"

	"google.golang.org/grpc"
)

func main() {
	cfg, err := config.LoadSkill()
	if err != nil {
		panic(err)
	}
	logger := obs.InitLogger(cfg.LogFormat, cfg.LogLevel)
	idx, err := (skill.Indexer{Root: cfg.SkillRoot}).Load()
	if err != nil {
		logger.Warn("skill index empty", "root", cfg.SkillRoot, "err", err)
	}
	topK := cfg.SkillTopK
	if topK <= 0 {
		topK = 3
	}
	svc := &skillService{selector: skill.RuleSelector{Index: idx, TopK: topK}}
	lis, err := net.Listen("tcp", cfg.SkillGRPCAddr)
	if err != nil {
		logger.Error("listen", "err", err)
		os.Exit(1)
	}
	srv := grpc.NewServer()
	pb.RegisterSkillServiceServer(srv, svc)
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	telemetry, err := obs.InitTelemetry(rootCtx, obs.TelemetryConfig{
		ServiceName:        cfg.OTELServiceName,
		DefaultServiceName: "skilld",
		OTELEnabled:        cfg.OTELEnabled,
		OTLPEndpoint:       cfg.OTELExporterOTLPEndpoint,
		MetricsEnabled:     cfg.MetricsEnabled,
		MetricsAddr:        cfg.MetricsAddr,
		MetricsPath:        cfg.MetricsPath,
	}, logger)
	if err != nil {
		logger.Warn("telemetry init", "err", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = telemetry.Shutdown(shutdownCtx)
		cancel()
	}()
	if cfg.DiscoveryEnabled {
		reg, err := discovery.Register(rootCtx, cfg.EtcdEndpoints, discovery.Instance{
			Service: "skilld",
			ID:      hostnameID("skilld"),
			Addr:    cfg.SkillGRPCAddr,
		}, 10)
		if err != nil {
			logger.Error("discovery register", "err", err)
		} else {
			defer reg.Close()
		}
	}
	go func() {
		logger.Info("skilld serving", "addr", cfg.SkillGRPCAddr, "skills", len(idx.Skills))
		if err := srv.Serve(lis); err != nil {
			logger.Error("serve", "err", err)
			stop()
		}
	}()
	<-rootCtx.Done()
	srv.GracefulStop()
}

func hostnameID(prefix string) string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return prefix
	}
	return prefix + "-" + name
}

type skillService struct {
	pb.UnimplementedSkillServiceServer
	selector skill.Selector
}

func (s *skillService) SelectSkills(ctx context.Context, req *pb.SelectSkillsRequest) (*pb.SelectSkillsResponse, error) {
	selected, err := s.selector.Select(ctx, req.GetQuery())
	if err != nil {
		return nil, err
	}
	out := make([]*pb.SkillDoc, 0, len(selected))
	for _, sk := range selected {
		out = append(out, &pb.SkillDoc{
			Name:        sk.Name,
			Description: sk.Description,
			Sha256:      sk.SHA256,
			Path:        sk.Path,
			Content:     sk.Content,
		})
	}
	return &pb.SelectSkillsResponse{Skills: out}, nil
}
