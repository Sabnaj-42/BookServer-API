package cmd

import (
	ap "github.com/Sabnaj-42/BookServer-API/apiHandler"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var (
	port     int
	startCmd = &cobra.Command{
		Use:   "start",
		Short: "start cmd starts the sever on a port",
		Long: `It starts the sever on a given posrt number  
                   post number will be given in the cmd`,

		Run: func(cmd *cobra.Command, args []string) {
			ap.RunServer(port)
		},
	}
)

func init() {
	rootCmd.AddCommand(startCmd)
	startCmd.PersistentFlags().IntVarP(&port, "port", "p", 8080, "port to listen on")
}
