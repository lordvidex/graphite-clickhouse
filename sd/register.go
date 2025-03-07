package sd

import (
	"os"
	"strings"
	"time"

	"github.com/lomik/graphite-clickhouse/config"
	"github.com/lomik/graphite-clickhouse/load_avg"
	"github.com/lomik/graphite-clickhouse/sd/nginx"
	"github.com/lomik/graphite-clickhouse/sd/utils"
	"go.uber.org/zap"
)

var (
	// ctxMain, Stop               = context.WithCancel(context.Background())
	stop     chan struct{} = make(chan struct{}, 1)
	delay                  = time.Second * 10
	hostname string
)

type SD interface {
	// Update update node record
	Update(listenIP, listenPort string, dc []string, weight int64) error
	// Delete delete node record (with previous IP/port)
	Delete(ip, port string, dc []string) error
	// Clear clear node record (all except with current listen IP/port)
	Clear(listenIP, listenPort string) error
	// Nodes return all registered nodes
	Nodes() (nodes []utils.KV, err error)
}

func Register(cfg *config.Config, logger *zap.Logger) {
	var (
		listenIP      string
		prevIP        string
		registerFirst bool
		sd            SD
		err           error
		load          float64
		w             int64
	)
	if cfg.Common.SD != "" {
		if strings.HasPrefix(cfg.Common.Listen, ":") {
			registerFirst = true
			listenIP = utils.GetLocalIP()
			prevIP = listenIP
		}
		hostname, _ = os.Hostname()
		hostname, _, _ = strings.Cut(hostname, ".")

		switch cfg.Common.SDType {
		case config.SDNginx:
			sd = nginx.New(cfg.Common.SD, cfg.Common.SDNamespace, hostname, logger)
		default:
			panic("serive discovery type not registered")
		}
		load, err = load_avg.Normalized()
		if err == nil {
			load_avg.Store(load)
		}

		logger.Info("init sd",
			zap.String("hostname", hostname),
		)

		w = load_avg.Weight(cfg.Common.BaseWeight, load)
		sd.Update(listenIP, cfg.Common.Listen, cfg.Common.SDDc, w)
		sd.Clear(listenIP, cfg.Common.Listen)
	}
LOOP:
	for {
		load, err = load_avg.Normalized()
		if err == nil {
			load_avg.Store(load)
		}
		if sd != nil {
			w = load_avg.Weight(cfg.Common.BaseWeight, load)

			if registerFirst {
				// if listen on all ip, try to register with first ip
				listenIP = utils.GetLocalIP()
			}

			sd.Update(listenIP, cfg.Common.Listen, cfg.Common.SDDc, w)

			if prevIP != listenIP {
				sd.Delete(prevIP, cfg.Common.Listen, cfg.Common.SDDc)
				prevIP = listenIP
			}
		}
		t := time.After(delay)
		select {
		case <-t:
			continue
		case <-stop:
			break LOOP
		}
	}

	if sd != nil {
		if err := sd.Clear("", ""); err == nil {
			logger.Info("cleanup sd",
				zap.String("hostname", hostname),
			)
		} else {
			logger.Warn("cleanup sd",
				zap.String("hostname", hostname),
				zap.Error(err),
			)
		}
	}
}

func Stop() {
	stop <- struct{}{}
}
