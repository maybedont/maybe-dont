package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var (
	cfgFile string
	logger  *zap.Logger
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "mcp-security-proxy",
	Short: "MCP Security Proxy - Enterprise-grade security controls for MCP communications",
	Long:  `A Go-based middleware service that provides enterprise-grade security controls for Model Context Protocol (MCP) communications.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./config.yaml)")
	rootCmd.PersistentFlags().StringP("log-level", "l", "info", "logging verbosity (debug, info, warn, error)")
	rootCmd.PersistentFlags().String("log-format", "json", "output format (json, text)")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "enable verbose output")

	// Bind flags to viper
	viper.BindPFlag("logging.level", rootCmd.PersistentFlags().Lookup("log-level"))
	viper.BindPFlag("logging.format", rootCmd.PersistentFlags().Lookup("log-format"))
	viper.BindPFlag("logging.verbose", rootCmd.PersistentFlags().Lookup("verbose"))
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Search config in home directory with name ".mcp-proxy" (without extension).
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.mcp-proxy")
		viper.AddConfigPath("/etc/mcp-proxy")
		viper.SetConfigName("config")
	}

	// Read in environment variables that match
	viper.SetEnvPrefix("MCP_PROXY")
	viper.AutomaticEnv()

	// Initialize logger
	initLogger()

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		logger.Info("Using config file", zap.String("file", viper.ConfigFileUsed()))
	} else {
		logger.Warn("No config file found, using defaults")
	}
}

func initLogger() {
	var err error
	config := zap.NewProductionConfig()

	// Set log level
	level := viper.GetString("logging.level")
	if level != "" {
		if err := config.Level.UnmarshalText([]byte(level)); err != nil {
			fmt.Printf("Invalid log level: %s\n", err)
			os.Exit(1)
		}
	}

	// Set log format
	if viper.GetString("logging.format") == "text" {
		config.Encoding = "console"
	}

	logger, err = config.Build()
	if err != nil {
		fmt.Printf("Failed to initialize logger: %s\n", err)
		os.Exit(1)
	}
}
