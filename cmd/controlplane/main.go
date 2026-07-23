package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/config"
	"github.com/wzhnbsixsixsix/agentforge/internal/controlplane"
	redisstore "github.com/wzhnbsixsixsix/agentforge/internal/storage/redis"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	cfg, err := config.LoadControlPlane()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	store, err := controlplane.OpenStore(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatal("controlplane postgres: ", err)
	}
	defer store.Close()
	rdb, err := redisstore.New(ctx, redisstore.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB})
	if err != nil {
		log.Fatal("controlplane redis: ", err)
	}
	defer rdb.Close()
	docker, err := controlplane.NewDockerManager()
	if err != nil {
		log.Fatal("controlplane docker: ", err)
	}
	defer docker.Close()
	conn, err := grpc.NewClient(cfg.GatewayDial, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal("controlplane gateway: ", err)
	}
	defer conn.Close()
	svc := controlplane.NewService(store, docker, controlplane.NewEventStore(rdb), conn, cfg.AgentDefaultImage)
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: (&controlplane.HTTPServer{Service: svc, StaticDir: cfg.WebDistDir}).Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shut, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = server.Shutdown(shut)
	}()
	log.Printf("controlplane serving on %s", cfg.HTTPAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
