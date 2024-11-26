package main

import (
	"log"
	"os"
	"os/signal"
	"path"
	"regexp"
	"syscall"
	"time"

	"github.com/opplieam/bb-dist-noti/internal/agent"
	"github.com/opplieam/bb-dist-noti/internal/tlsconfig"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type config struct {
	AConfig agent.Config

	ClusterRun    bool
	ServerTLSPath tlsconfig.TLSConfig
	PeerTLSPath   tlsconfig.TLSConfig
}

func main() {
	cfg := &config{}

	cmd := &cobra.Command{
		Use:     "bb-noti",
		PreRunE: cfg.setupConfig,
		RunE:    cfg.run,
	}
	if err := setupFlags(cmd); err != nil {
		log.Fatal(err)
	}
	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func setupFlags(cmd *cobra.Command) error {
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	dataDir := path.Join(os.TempDir(), "bb-noti")
	cmd.Flags().String("data-dir", dataDir, "Directory to store Raft log consensus data")
	cmd.Flags().String("node-name", hostname, "Unique server ID")
	cmd.Flags().String("serf-addr", "127.0.0.1:8401", "Address to bind Serf on")
	cmd.Flags().Int("rpc-port", 8400, "Port for RPC (and Raft) connections")
	cmd.Flags().StringSlice("start-join-addrs", nil, "Serf addresses to join")
	cmd.Flags().Bool("bootstrap", false, "Bootstrap the cluster")
	cmd.Flags().Bool("cluster-run", false, "Is Running in cluster")

	cmd.Flags().String("http-addr", ":5000", "Http listen address")
	cmd.Flags().Duration("http-write-timeout", 60*time.Second, "Http Write timeout")
	cmd.Flags().Duration("http-read-timeout", 10*time.Second, "Http Read timeout")
	cmd.Flags().Duration("http-idle-timeout", 600*time.Second, "Http Idle timeout")
	cmd.Flags().Duration("http-shutdown-timeout", 20*time.Second, "Http Shutdown timeout")

	cmd.Flags().String("server-tls-cert-file", "", "Path to server tls cert")
	cmd.Flags().String("server-tls-key-file", "", "Path to server tls key")
	cmd.Flags().String("server-tls-ca-file", "", "Path to server certificate authority")
	cmd.Flags().String("peer-tls-cert-file", "", "Path to peer tls cert")
	cmd.Flags().String("peer-tls-key-file", "", "Path to peer tls key")
	cmd.Flags().String("peer-tls-ca-file", "", "Path to peer certificate authority")

	return viper.BindPFlags(cmd.Flags())
}

func (c *config) setupConfig(cmd *cobra.Command, args []string) error {
	c.AConfig.DataDir = viper.GetString("data-dir")
	c.AConfig.NodeName = viper.GetString("node-name")
	c.AConfig.SerfAddr = viper.GetString("serf-addr")
	c.AConfig.RPCPort = viper.GetInt("rpc-port")
	c.AConfig.Bootstrap = viper.GetBool("bootstrap")

	c.AConfig.HttpConfig.Addr = viper.GetString("http-addr")
	c.AConfig.HttpConfig.ReadTimeout = viper.GetDuration("http-read-timeout")
	c.AConfig.HttpConfig.WriteTimeout = viper.GetDuration("http-write-timeout")
	c.AConfig.HttpConfig.IdleTimeout = viper.GetDuration("http-idle-timeout")
	c.AConfig.HttpConfig.ShutdownTimeout = viper.GetDuration("http-shutdown-timeout")

	// Setup TLS
	c.ServerTLSPath.CertFile = viper.GetString("server-tls-cert-file")
	c.ServerTLSPath.KeyFile = viper.GetString("server-tls-key-file")
	c.ServerTLSPath.CAFile = viper.GetString("server-tls-ca-file")
	c.PeerTLSPath.CertFile = viper.GetString("peer-tls-cert-file")
	c.PeerTLSPath.KeyFile = viper.GetString("peer-tls-key-file")
	c.PeerTLSPath.CAFile = viper.GetString("peer-tls-ca-file")

	var err error
	if c.ServerTLSPath.CertFile != "" && c.ServerTLSPath.KeyFile != "" {
		c.ServerTLSPath.Server = true
		c.AConfig.ServerTLSConfig, err = tlsconfig.SetupTLSConfig(c.ServerTLSPath)
		if err != nil {
			return err
		}
	}

	if c.PeerTLSPath.CertFile != "" && c.PeerTLSPath.KeyFile != "" {
		c.PeerTLSPath.Server = false
		c.AConfig.PeerTLSConfig, err = tlsconfig.SetupTLSConfig(c.PeerTLSPath)
		if err != nil {
			return err
		}
	}

	// Setup Start join address
	c.ClusterRun = viper.GetBool("cluster-run")
	if c.ClusterRun {
		// hostname = bb-noti-0 (k8s cluster)
		hostname, err := os.Hostname()
		if err != nil {
			return err
		}
		re := regexp.MustCompile(`-(\d+)$`)
		match := re.FindStringSubmatch(hostname)

		// Setup Bootstrap only for the node bb-noti-0
		id := match[1]
		c.AConfig.Bootstrap = id == "0"

		// Setup Start join address
		// TODO: Better define joining address
		// Don't set StartJoinAddrs at Node 0
		if id != "0" {
			c.AConfig.StartJoinAddrs = viper.GetStringSlice("start-join-addrs")
		}

	} else {
		startJoinAddrs := viper.GetStringSlice("start-join-addrs")
		if startJoinAddrs == nil {
			startJoinAddrs = []string{"127.0.0.1:8401"}
		}
		c.AConfig.StartJoinAddrs = startJoinAddrs
	}

	return nil
}

func (c *config) run(cmd *cobra.Command, args []string) error {
	a, err := agent.NewAgent(c.AConfig)
	if err != nil {
		return err
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	return a.Shutdown()
}
