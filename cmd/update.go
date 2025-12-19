package cmd

import (
	"deploy-cli/internal/manager"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newUpdateCmd(getManager func() manager.ServiceManager) *cobra.Command {
	return &cobra.Command{
		Use:   "update <service-name> <new-path>",
		Short: "Update a registered service's path",
		Long:  `Update the directory path for an existing registered service.`,
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			serviceName := args[0]
			newProjectPath := args[1]
			mgr := getManager()

			cmd.Printf("Updating service '%s' to path '%s'\n", serviceName, newProjectPath)

			exists, err := mgr.IsServiceRegistered(serviceName)
			if err != nil {
				cmd.Printf("Error checking service: %v\n", err)
				return
			} else if !exists {
				cmd.Println("The service isn't registered")
				cmd.Println("- Use 'deploy-cli add <path>' to register services")
				cmd.Println("- Use 'deploy-cli status' to view registered services")
				return
			} else {
				yamlFile, err := determineYamlFile(newProjectPath)

				if err != nil {
					cmd.Printf("Error determining YAML file on %v\n", newProjectPath)
					return
				}

				absPath, err := filepath.Abs(filepath.Dir(yamlFile))
				if err != nil {
					cmd.Printf("Error getting absolute path: %v\n", err)
					return
				}

				err = mgr.UpdateServiceCatalogEntry(serviceName, absPath, filepath.Base(yamlFile))
				if err != nil {
					cmd.Printf("Error updating the service: %v\n", err)
				} else {
					cmd.Printf("Successfully updated the service %s", serviceName)
				}
			}
		}}
}
