package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/hashicorp/vault/api"
	log "github.com/sirupsen/logrus"
	"github.com/tuenti/secrets-manager/errors"

	"github.com/prometheus/client_golang/prometheus"
)

var logger *log.Logger

const defaultSecretKey = "data"

var (
	vaultLabelNames = []string{"vault_address", "vault_engine", "vault_version", "vault_cluster_id", "vault_cluster_name"}
	vaultLabels     = make(map[string]string, len(vaultLabelNames))

	// Prometeheus metrics: https://prometheus.io
	tokenExpired = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "secrets_manager",
		Subsystem: "vault",
		Name:      "token_expired",
		Help:      "The state of the token: 1 = expired; 0 = still valid",
	}, vaultLabelNames)
	tokenTTL = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "secrets_manager",
		Subsystem: "vault",
		Name:      "token_ttl",
		Help:      "Vault token TTL",
	}, vaultLabelNames)
)

type client struct {
	vclient            *api.Client
	logical            *api.Logical
	maxTokenTTL        int64
	tokenPollingPeriod time.Duration
	renewTTLIncrement  int
	engine             engine
}

func init() {
	prometheus.MustRegister(tokenExpired)
	prometheus.MustRegister(tokenTTL)
}

func vaultClient(ctx context.Context, l *log.Logger, cfg Config) (*client, error) {
	if l != nil {
		logger = l
	} else {
		logger = log.New()
	}

	httpClient := new(http.Client)
	vclient, err := api.NewClient(&api.Config{Address: cfg.VaultURL, HttpClient: httpClient})

	if err != nil {
		logger.Debugf("unable to build vault client: %v", err)
		return nil, err
	}

	vclient.SetToken(cfg.VaultToken)
	sys := vclient.Sys()
	health, err := sys.Health()

	if err != nil {
		logger.Debugf("could not contact Vault at %s: %v ", cfg.VaultURL, err)
		return nil, err
	}

	logger.Infof("successfully logged in to Vault cluster %s", health.ClusterName)
	logical := vclient.Logical()

	engine, err := newEngine(cfg.VaultEngine)
	if err != nil {
		logger.Debugf("unable to use engine %s: %v", cfg.VaultEngine, err)
		return nil, err
	}

	vaultLabels["vault_address"] = cfg.VaultURL
	vaultLabels["vault_engine"] = cfg.VaultEngine
	vaultLabels["vault_version"] = health.Version
	vaultLabels["vault_cluster_id"] = health.ClusterID
	vaultLabels["vault_cluster_name"] = health.ClusterName

	client := client{
		vclient:            vclient,
		logical:            logical,
		maxTokenTTL:        cfg.VaultMaxTokenTTL,
		tokenPollingPeriod: cfg.VaultTokenPollingPeriod,
		renewTTLIncrement:  cfg.VaultRenewTTLIncrement,
		engine:             engine,
	}
	client.startTokenRenewer(ctx)
	return &client, err
}

func (c *client) isTokenExpired() bool {
	var ttl int64
	exp := false
	auth := c.vclient.Auth()

	lookup, err := auth.Token().LookupSelf()
	if err != nil {
		logger.Errorf("error checking token with lookup self api: %v", err)
		exp = true
		return exp
	}

	isRenewable, err := lookup.TokenIsRenewable()

	if err != nil {
		logger.Errorf("could not check token renewability: %v", err)
		exp = true
		return exp
	}

	if isRenewable {
		ttl, err = lookup.Data["ttl"].(json.Number).Int64()
		if err != nil {
			logger.Errorf("couldn't decode ttl from token: %v", err)
			exp = true
			return exp
		}

		tokenTTL.WithLabelValues(
			vaultLabels["vault_address"],
			vaultLabels["vault_engine"],
			vaultLabels["vault_version"],
			vaultLabels["vault_cluster_id"],
			vaultLabels["vault_cluster_name"]).Set(float64(ttl))

		if ttl < c.maxTokenTTL {
			logger.Warnf("token is really close to expire, current ttl: %d", ttl)
			exp = true
			return exp
		}
	}

	// Update tokenExpired metric
	if exp {
		tokenExpired.WithLabelValues(
			vaultLabels["vault_address"],
			vaultLabels["vault_engine"],
			vaultLabels["vault_version"],
			vaultLabels["vault_cluster_id"],
			vaultLabels["vault_cluster_name"]).Set(1)
	} else {
		tokenExpired.WithLabelValues(
			vaultLabels["vault_address"],
			vaultLabels["vault_engine"],
			vaultLabels["vault_version"],
			vaultLabels["vault_cluster_id"],
			vaultLabels["vault_cluster_name"]).Set(0)
	}

	return exp
}

func (c *client) renewToken() error {
	auth := c.vclient.Auth()
	_, err := auth.Token().RenewSelf(c.renewTTLIncrement)
	return err
}

func (c *client) startTokenRenewer(ctx context.Context) {
	go func(ctx context.Context) {
		for {
			select {
			case <-time.After(c.tokenPollingPeriod):
				if c.isTokenExpired() {
					err := c.renewToken()
					if err != nil {
						logger.Errorf("could not renew token: %v", err)
					} else {
						logger.Infoln("token renewed successfully!")
					}
				}
			case <-ctx.Done():
				logger.Infoln("gracefully shutting down token renewal go routine")
				return
			}
		}
	}(ctx)
}

func (c *client) ReadSecret(path string, key string) (string, error) {
	data := ""
	if key == "" {
		key = defaultSecretKey
	}

	logical := c.logical
	secret, err := logical.Read(path)
	if err != nil {
		return data, err
	}

	if secret != nil {
		secretData := c.engine.getData(secret)
		warnings := secret.Warnings
		if secretData != nil {
			if secretData[key] != nil {
				data = secretData[key].(string)
			} else {
				err = &errors.BackendSecretNotFoundError{ErrType: errors.BackendSecretNotFoundErrorType, Path: path, Key: key}
			}
		} else {
			for _, w := range warnings {
				logger.Warningln(w)
			}
			err = &errors.BackendSecretNotFoundError{ErrType: errors.BackendSecretNotFoundErrorType, Path: path, Key: key}
		}
	} else {
		err = &errors.BackendSecretNotFoundError{ErrType: errors.BackendSecretNotFoundErrorType, Path: path, Key: key}
	}
	return data, err
}
