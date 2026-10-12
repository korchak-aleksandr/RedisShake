package config

import (
	"RedisShake/internal/log"
	"bytes"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/mcuadros/go-defaults"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

type FilterOptions struct {
	AllowKeys         []string `mapstructure:"allow_keys" default:"[]"`
	AllowKeyPrefix    []string `mapstructure:"allow_key_prefix" default:"[]"`
	AllowKeySuffix    []string `mapstructure:"allow_key_suffix" default:"[]"`
	BlockKeys         []string `mapstructure:"block_keys" default:"[]"`
	BlockKeyPrefix    []string `mapstructure:"block_key_prefix" default:"[]"`
	BlockKeySuffix    []string `mapstructure:"block_key_suffix" default:"[]"`
	AllowKeyRegex     []string `mapstructure:"allow_key_regex" default:"[]"`
	BlockKeyRegex     []string `mapstructure:"block_key_regex" default:"[]"`
	AllowDB           []int    `mapstructure:"allow_db" default:"[]"`
	BlockDB           []int    `mapstructure:"block_db" default:"[]"`
	AllowCommand      []string `mapstructure:"allow_command" default:"[]"`
	BlockCommand      []string `mapstructure:"block_command" default:"[]"`
	AllowCommandGroup []string `mapstructure:"allow_command_group" default:"[]"`
	BlockCommandGroup []string `mapstructure:"block_command_group" default:"[]"`
	Function          string   `mapstructure:"function" default:""`
}

type AdvancedOptions struct {
	Dir string `mapstructure:"dir" default:"data"`

	Ncpu int `mapstructure:"ncpu" default:"0"`

	PprofPort  int `mapstructure:"pprof_port" default:"0"`
	StatusPort int `mapstructure:"status_port" default:"0"`

	// log
	LogFile       string `mapstructure:"log_file" default:"shake.log"`
	LogLevel      string `mapstructure:"log_level" default:"info"`
	LogInterval   int    `mapstructure:"log_interval" default:"5"`
	LogRotation   bool   `mapstructure:"log_rotation" default:"true"`
	LogMaxSize    int    `mapstructure:"log_max_size" default:"512"`
	LogMaxAge     int    `mapstructure:"log_max_age" default:"7"`
	LogMaxBackups int    `mapstructure:"log_max_backups" default:"3"`
	LogCompress   bool   `mapstructure:"log_compress" default:"true"`

	// redis-shake gets key and value from rdb file, and uses RESTORE command to
	// create the key in target redis. Redis RESTORE will return a "Target key name
	// is busy" error when key already exists. You can use this configuration item
	// to change the default behavior of restore:
	// panic:   redis-shake will stop when meet "Target key name is busy" error.
	// rewrite: redis-shake will replace the key with new value.
	// skip:    redis-shake will skip restore the key when meet "Target key name is busy" error.
	//
	// Applicability:
	// - rdb_reader:     Applies (uses RESTORE command)
	// - sync_reader:    Only applies to RDB phase. AOF phase forwards commands directly without RESTORE.
	// - scan_reader:    Applies (uses RESTORE command)
	//
	// Limitation:
	// For large values exceeding target_redis_proto_max_bulk_len, individual commands (SET, HSET, etc.)
	// are used instead of RESTORE, which may not respect this setting.
	RDBRestoreCommandBehavior string `mapstructure:"rdb_restore_command_behavior" default:"panic"`

	PipelineCountLimit              uint64 `mapstructure:"pipeline_count_limit" default:"1024"`
	TargetRedisClientMaxQuerybufLen int64  `mapstructure:"target_redis_client_max_querybuf_len" default:"1024000000"`
	TargetRedisProtoMaxBulkLen      uint64 `mapstructure:"target_redis_proto_max_bulk_len" default:"512000000"`
	TargetRedisMaxQPS               int    `mapstructure:"target_redis_max_qps" default:"300000"`

	AwsPSync string `mapstructure:"aws_psync" default:""` // 10.0.0.1:6379@nmfu2sl5osync,10.0.0.1:6379@xhma21xfkssync

	EmptyDBBeforeSync bool `mapstructure:"empty_db_before_sync" default:"false"`

	// Timeout of a single attempt to connect to Redis, including DNS lookup and TLS handshake.
	DialTimeoutSec int `mapstructure:"dial_timeout_sec" default:"10"`

	// How long a connection stays idle before the first TCP keepalive probe. Probes then
	// follow Go defaults: every 15 seconds, 9 times. Keeps idle connections alive through
	// NAT and VPN gateways that drop silent flows.
	TcpKeepaliveSec int `mapstructure:"tcp_keepalive_sec" default:"30"`
}

type ModuleOptions struct {
	TargetMBbloomVersion int `mapstructure:"target_mbbloom_version" default:"0"` // v1.0.0 <=> 10000
}

func (opt *AdvancedOptions) GetPSyncCommand(address string) string {
	items := strings.Split(opt.AwsPSync, ",")
	for _, item := range items {
		if strings.HasPrefix(item, address) {
			return strings.Split(item, "@")[1]
		}
	}
	log.Panicf("can not find aws psync command. address=[%s],aws_psync=[%s]", address, opt.AwsPSync)
	return ""
}

type ShakeOptions struct {
	Filter   FilterOptions
	Advanced AdvancedOptions
	Module   ModuleOptions
}

var Opt ShakeOptions

func LoadConfig() *viper.Viper {
	defaults.SetDefaults(&Opt)

	var v *viper.Viper
	if len(os.Args) > 2 {
		fmt.Println("Usage: redis-shake [config file]")
		fmt.Println("Example: ")
		fmt.Println(" 		redis-shake sync.toml # load config from sync.toml")
		fmt.Println("		redis-shake 		  # load config from environment variables")
		os.Exit(1)
	}
	consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "2006-01-02 15:04:05"}
	logger := zerolog.New(consoleWriter).With().Timestamp().Logger()
	// load config from file
	if len(os.Args) == 2 {
		logger.Info().Msgf("load config from file: %s", os.Args[1])
		var err error
		if v, err = readConfig(os.Args[1]); err != nil {
			logger.Error().Msgf("failed to read config file: %v", err)
			os.Exit(1)
		}
	} else {
		logger.Error().Msg("config file not found")
		os.Exit(1)
	}

	// unmarshal config
	err := v.Unmarshal(&Opt)
	if err != nil {
		panic(err)
	}
	return v
}

// readConfig reads a TOML config file, substituting environment variables.
func readConfig(path string) (*viper.Viper, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader([]byte(envWithFallback(string(file))))); err != nil {
		return nil, err
	}
	return v, nil
}

// WatchConfig calls onChange with the reloaded config each time the content of the config file
// changes, including when Kubernetes updates a mounted ConfigMap. A file that fails to parse is
// logged and skipped.
func WatchConfig(path string, onChange func(v *viper.Viper)) {
	last, err := os.ReadFile(path)
	if err != nil {
		log.Warnf("config watch disabled, failed to read config file. path=[%s], err=[%v]", path, err)
		return
	}
	w := viper.New()
	w.SetConfigFile(path)
	w.OnConfigChange(func(fsnotify.Event) {
		content, err := os.ReadFile(path)
		if err != nil || bytes.Equal(content, last) {
			return
		}
		last = content
		v, err := readConfig(path)
		if err != nil {
			log.Warnf("config file changed but failed to parse, keeping the current config. err=[%v]", err)
			return
		}
		onChange(v)
	})
	w.WatchConfig()
}

// ChangedKeys returns the keys whose values differ between two configs, sorted.
func ChangedKeys(old, new *viper.Viper) []string {
	keys := make(map[string]struct{})
	for _, k := range old.AllKeys() {
		keys[k] = struct{}{}
	}
	for _, k := range new.AllKeys() {
		keys[k] = struct{}{}
	}
	changed := make([]string, 0)
	for k := range keys {
		if !reflect.DeepEqual(old.Get(k), new.Get(k)) {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	return changed
}

// Custom substitution function that returns as is if the environment variable is empty
func envWithFallback(input string) string {
	re := regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*?)}`)
	return re.ReplaceAllStringFunc(input, func(match string) string {
		varName := match[1:]
		if strings.HasPrefix(varName, "{") && strings.HasSuffix(varName, "}") {
			varName = varName[1 : len(varName)-1] // Remove { and }
		}
		value := os.Getenv(varName)
		if value == "" {
			return match // if the environment variable is empty return as is
		}
		return value
	})
}
