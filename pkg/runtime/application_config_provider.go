package runtime

import (
	"fmt"

	"github.com/flyteorg/flyteadmin/pkg/common"
	"github.com/flyteorg/flyteadmin/pkg/runtime/interfaces"
	"github.com/flyteorg/flytestdlib/config"
	"github.com/flyteorg/flytestdlib/database"
)

const flyteAdmin = "flyteadmin"
const scheduler = "scheduler"
const remoteData = "remoteData"
const notifications = "notifications"
const domains = "domains"
const externalEvents = "externalEvents"
const cloudEvents = "cloudEvents"
const metricPort = 10254

const KB = 1024
const MB = KB * KB

var flyteAdminConfig = config.MustRegisterSection(flyteAdmin, &interfaces.ApplicationConfig{
	ProfilerPort:          metricPort,
	MetricsScope:          "flyte:",
	MetadataStoragePrefix: []string{"metadata", "admin"},
	EventVersion:          2,
	AsyncEventsBufferSize: 100,
	MaxParallelism:        25,
	K8SServiceAccount:     "default",
})

var schedulerConfig = config.MustRegisterSection(scheduler, &interfaces.SchedulerConfig{
	ProfilerPort: config.Port{Port: metricPort},
	EventSchedulerConfig: interfaces.EventSchedulerConfig{
		Scheme:               common.Local,
		FlyteSchedulerConfig: &interfaces.FlyteSchedulerConfig{},
	},
	WorkflowExecutorConfig: interfaces.WorkflowExecutorConfig{
		Scheme: common.Local,
		FlyteWorkflowExecutorConfig: &interfaces.FlyteWorkflowExecutorConfig{
			AdminRateLimit: &interfaces.AdminRateLimit{
				Tps:   100,
				Burst: 10,
			},
		},
	},
})
var remoteDataConfig = config.MustRegisterSection(remoteData, &interfaces.RemoteDataConfig{
	Scheme:                common.None,
	MaxSizeInBytes:        2 * MB,
	InlineEventDataPolicy: interfaces.InlineEventDataPolicyOffload,
	SignedURL: interfaces.SignedURL{
		Enabled: false,
	},
})
var notificationsConfig = config.MustRegisterSection(notifications, &interfaces.NotificationsConfig{
	Type: common.Local,
})
var domainsConfig = config.MustRegisterSection(domains, &interfaces.DomainsConfig{
	{
		ID:   "development",
		Name: "development",
	},
	{
		ID:   "staging",
		Name: "staging",
	},
	{
		ID:   "production",
		Name: "production",
	},
})
var externalEventsConfig = config.MustRegisterSection(externalEvents, &interfaces.ExternalEventsConfig{
	Type: common.Local,
})

var cloudEventsConfig = config.MustRegisterSection(cloudEvents, &interfaces.CloudEventsConfig{
	Type: common.Local,
})

// Implementation of an interfaces.ApplicationConfiguration
type ApplicationConfigurationProvider struct{}

func (p *ApplicationConfigurationProvider) GetDbConfig() *interfaces.DbConfig {
	databaseConfig := database.GetConfig()
	switch {
	case !databaseConfig.SQLite.IsEmpty():
		sqliteConfig := interfaces.SQLiteConfig(databaseConfig.SQLite)
		return &interfaces.DbConfig{SQLiteConfig: &sqliteConfig}
	case !databaseConfig.Postgres.IsEmpty():
		postgresConfig := interfaces.PostgresConfig(databaseConfig.Postgres)
		return &interfaces.DbConfig{PostgresConfig: &postgresConfig}
	default:
		panic(fmt.Errorf("database config cannot be empty"))
	}
}

func (p *ApplicationConfigurationProvider) GetTopLevelConfig() *interfaces.ApplicationConfig {
	return flyteAdminConfig.GetConfig().(*interfaces.ApplicationConfig)
}

func (p *ApplicationConfigurationProvider) GetSchedulerConfig() *interfaces.SchedulerConfig {
	return schedulerConfig.GetConfig().(*interfaces.SchedulerConfig)
}

func (p *ApplicationConfigurationProvider) GetRemoteDataConfig() *interfaces.RemoteDataConfig {
	return remoteDataConfig.GetConfig().(*interfaces.RemoteDataConfig)
}

func (p *ApplicationConfigurationProvider) GetNotificationsConfig() *interfaces.NotificationsConfig {
	return notificationsConfig.GetConfig().(*interfaces.NotificationsConfig)
}

func (p *ApplicationConfigurationProvider) GetDomainsConfig() *interfaces.DomainsConfig {
	return domainsConfig.GetConfig().(*interfaces.DomainsConfig)
}

func (p *ApplicationConfigurationProvider) GetExternalEventsConfig() *interfaces.ExternalEventsConfig {
	return externalEventsConfig.GetConfig().(*interfaces.ExternalEventsConfig)
}

func (p *ApplicationConfigurationProvider) GetCloudEventsConfig() *interfaces.CloudEventsConfig {
	return cloudEventsConfig.GetConfig().(*interfaces.CloudEventsConfig)
}

func NewApplicationConfigurationProvider() interfaces.ApplicationConfiguration {
	return &ApplicationConfigurationProvider{}
}
