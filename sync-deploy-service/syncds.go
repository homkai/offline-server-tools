package main

import (
	"github.com/spf13/cobra"
	"log"
)

//func main2() {
func main() {
	var cmdClient = &cobra.Command{
		Use:   "client",
		Short: "to start a client",
		Long: `to start a client. watch local files change. sync files and send deploy command to server`,
		Args: cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			var conf ClientConf
			conf.getConf()
			StartClient(conf)
		},
	}

	var cmdServer = &cobra.Command{
		Use:   "server",
		Short: "to start a server",
		Long: `to start a server. listen to client. receive the change file and exec the deploy command`,
		Args: cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			var conf ServerConf
			conf.getConf()
			StartServer(conf)
		},
	}

	var rootCmd = &cobra.Command{Use: "syncds"}
	rootCmd.AddCommand(cmdClient, cmdServer)
	err := rootCmd.Execute()
	if err != nil {
		log.Fatal("rootCmd err", err)
	}
}