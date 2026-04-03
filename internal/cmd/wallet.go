package cmd

import (
	"fmt"

	"github.com/atotto/clipboard"
	"github.com/kawai-network/x/jarvis"
	"github.com/kawai-network/y/walletsetup"
	"github.com/spf13/cobra"
)

var walletCmd = &cobra.Command{
	Use:   "wallet",
	Short: "Manage local wallet",
}

var walletSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Setup or import a wallet",
	RunE: func(cmd *cobra.Command, args []string) error {
		wallet := jarvis.NewWalletService("", nil)
		input := walletsetup.NewInputReader()

		result, err := walletsetup.RunInteractiveSetup(cliWalletUI{}, input, wallet, walletsetup.SetupOptions{
			CopyMnemonic: clipboard.WriteAll,
		})
		if err != nil {
			return err
		}

		if result.Address != "" {
			fmt.Printf("✅ Wallet: %s\n", result.Address)
		}
		return nil
	},
}

type cliWalletUI struct{}

func (cliWalletUI) Println(a ...any) {
	fmt.Println(a...)
}

func (cliWalletUI) Printf(format string, a ...any) {
	fmt.Printf(format, a...)
}

func init() {
	walletCmd.AddCommand(walletSetupCmd)
}
