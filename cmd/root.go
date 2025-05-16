/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "BookServer",
	Short: "Book Server API",
	Long:  `A Restful api server to store and show book information`,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

/*func init() {
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
*/
