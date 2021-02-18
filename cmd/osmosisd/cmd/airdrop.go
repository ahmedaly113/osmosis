package cmd

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	v036genaccounts "github.com/cosmos/cosmos-sdk/x/genaccounts/legacy/v036"
	v036staking "github.com/cosmos/cosmos-sdk/x/staking/legacy/v036"
)

const (
	flagSnapshotOutput = "snapshot-output"
)

// GenesisStateV036 is minimum structure to import airdrop accounts
type GenesisStateV036 struct {
	AppState AppStateV036 `json:"app_state"`
}

// AppStateV036 is app state structure for app state
type AppStateV036 struct {
	Accounts []v036genaccounts.GenesisAccount `json:"accounts"`
	Staking  v036staking.GenesisState         `json:"staking"`
}

// SnapshotFields provide fields of snapshot per account
type SnapshotFields struct {
	AtomAddress           string  `json:"atom_address"`
	AtomBalance           sdk.Int `json:"atom_balance"`
	AtomStakedBalance     sdk.Int `json:"atom_staked_balance"`
	AtomUnstakedBalance   sdk.Int `json:"atom_unstaked_balance"`
	AtomStakedPercent     sdk.Dec `json:"atom_staked_percent"`
	AtomOwnershipPercent  sdk.Dec `json:"atom_ownership_percent"`
	OsmoNormalizedBalance sdk.Int `json:"osmo_balance_normalized"`
	OsmoBalance           sdk.Int `json:"osmo_balance"`
	OsmoBalanceBonus      sdk.Int `json:"osmo_balance_bonus"`
	OsmoBalanceBase       sdk.Int `json:"osmo_balance_base"`
	OsmoPercent           sdk.Dec `json:"osmo_ownership_percent"`
}

// setCosmosBech32Prefixes set config for cosmos address system
func setCosmosBech32Prefixes() {
	defaultConfig := sdk.NewConfig()
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount(defaultConfig.GetBech32AccountAddrPrefix(), defaultConfig.GetBech32AccountPubPrefix())
	config.SetBech32PrefixForValidator(defaultConfig.GetBech32ValidatorAddrPrefix(), defaultConfig.GetBech32ValidatorPubPrefix())
	config.SetBech32PrefixForConsensusNode(defaultConfig.GetBech32ConsensusAddrPrefix(), defaultConfig.GetBech32ConsensusPubPrefix())
}

// ExportAirdropFromGenesisCmd returns add-genesis-account cobra Command.
func ExportAirdropFromGenesisCmd(defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export-airdrop-genesis [denom] [file] [totalAmount]",
		Short: "Import balances from provided genesis to {FlagHome}/genesis.json",
		Long: `Import balances from provided genesis to {FlagHome}/genesis.json
Download:
	https://raw.githubusercontent.com/cephalopodequipment/cosmoshub-3/master/genesis.json
Init genesis file:
	osmosisd init mynode
Example:
	osmosisd export-airdrop-genesis uatom ../genesis.json 100000000000000 --snapshot-output="../snapshot.json"
	- Check genesis:
		file is at ~/.osmosisd/config/genesis.json
	- Snapshot
		file is at "../snapshot.json"
`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)
			// depCdc := clientCtx.JSONMarshaler
			// cdc := depCdc.(codec.Marshaler)
			aminoCodec := clientCtx.LegacyAmino.Amino

			serverCtx := server.GetServerContextFromCmd(cmd)
			config := serverCtx.Config

			config.SetRoot(clientCtx.HomeDir)

			denom := args[0]
			filepath := args[1]
			// osdenom := "uosmo"
			snapshotOutput, err := cmd.Flags().GetString(flagSnapshotOutput)
			if err != nil {
				return fmt.Errorf("failed to get snapshot directory: %w", err)
			}

			// totalAmount, ok := sdk.NewIntFromString(args[2])
			// if !ok {
			// 	return fmt.Errorf("failed to parse totalAmount: %s", args[2])
			// }

			// genFile := config.GenesisFile()
			// appState, genDoc, err := genutiltypes.GenesisStateFromGenFile(genFile)
			// if err != nil {
			// 	return fmt.Errorf("failed to unmarshal genesis state: %w", err)
			// }

			// authGenState := authtypes.GetGenesisStateFromAppState(cdc, appState)

			// accs, err := authtypes.UnpackAccounts(authGenState.Accounts)
			// if err != nil {
			// 	return fmt.Errorf("failed to get accounts from any: %w", err)
			// }

			jsonFile, err := os.Open(filepath)
			if err != nil {
				return err
			}
			defer jsonFile.Close()

			byteValue, _ := ioutil.ReadAll(jsonFile)

			var genStateV036 GenesisStateV036

			setCosmosBech32Prefixes()
			err = aminoCodec.UnmarshalJSON(byteValue, &genStateV036)
			if err != nil {
				return err
			}

			snapshot := make(map[string]SnapshotFields)

			totalAtomBalance := sdk.NewInt(0)
			for _, account := range genStateV036.AppState.Accounts {

				balance := account.Coins.AmountOf(denom)
				totalAtomBalance = totalAtomBalance.Add(balance)

				snapshot[account.Address.String()] = SnapshotFields{
					AtomAddress:         account.Address.String(),
					AtomBalance:         balance,
					AtomUnstakedBalance: balance,
					AtomStakedBalance:   sdk.ZeroInt(),
				}
			}

			for _, unbonding := range genStateV036.AppState.Staking.UnbondingDelegations {
				address := unbonding.DelegatorAddress.String()
				acc, ok := snapshot[address]
				if !ok {
					panic("no account found for unbonding")
				}

				unbondingAtoms := sdk.NewInt(0)
				for _, entry := range unbonding.Entries {
					unbondingAtoms = unbondingAtoms.Add(entry.Balance)
				}

				acc.AtomBalance = acc.AtomBalance.Add(unbondingAtoms)
				acc.AtomUnstakedBalance = acc.AtomUnstakedBalance.Add(unbondingAtoms)
				totalAtomBalance = totalAtomBalance.Add(unbondingAtoms)

				snapshot[address] = acc
			}

			validators := make(map[string]v036staking.Validator)
			for _, validator := range genStateV036.AppState.Staking.Validators {
				validators[validator.OperatorAddress.String()] = validator
			}

			for _, delegation := range genStateV036.AppState.Staking.Delegations {
				address := delegation.DelegatorAddress.String()

				acc, ok := snapshot[address]
				if !ok {
					panic("no account found for delegation")
				}

				val := validators[delegation.ValidatorAddress.String()]
				stakedAtoms := delegation.Shares.MulInt(val.Tokens).Quo(val.DelegatorShares).RoundInt()

				acc.AtomBalance = acc.AtomBalance.Add(stakedAtoms)
				acc.AtomStakedBalance = acc.AtomStakedBalance.Add(stakedAtoms)
				totalAtomBalance = totalAtomBalance.Add(stakedAtoms)

				snapshot[address] = acc
			}

			totalOsmoBalance := sdk.NewInt(0)

			// fmt.Println(snapshot)

			onePointFive := sdk.MustNewDecFromStr("1.5") // sdk.NewDecFromIntWithPrec(sdk.NewInt(15), 1)

			for address, acc := range snapshot {
				allAtoms := acc.AtomBalance.ToDec()

				acc.AtomOwnershipPercent = allAtoms.QuoInt(totalAtomBalance)

				if allAtoms.IsZero() {
					acc.AtomStakedPercent = sdk.ZeroDec()
					acc.OsmoBalanceBase = sdk.ZeroInt()
					acc.OsmoBalanceBonus = sdk.ZeroInt()
					acc.OsmoBalance = sdk.ZeroInt()
					snapshot[address] = acc
					continue
				}

				stakedAtoms := acc.AtomStakedBalance.ToDec()
				stakedPercent := stakedAtoms.Quo(allAtoms)
				acc.AtomStakedPercent = stakedPercent

				baseOsmo, err := allAtoms.ApproxSqrt()
				if err != nil {
					// fmt.Println("failed to root atom balance", err)
					// continue
					panic(fmt.Sprintf("failed to root atom balance: %s", err))
				}
				acc.OsmoBalanceBase = baseOsmo.RoundInt()

				bonusOsmo := baseOsmo.Mul(onePointFive).Mul(stakedPercent)
				acc.OsmoBalanceBonus = bonusOsmo.RoundInt()

				allOsmo := baseOsmo.Add(bonusOsmo)
				acc.OsmoBalance = allOsmo.RoundInt()

				totalOsmoBalance = totalOsmoBalance.Add(allOsmo.RoundInt())

				if allAtoms.LT(sdk.OneDec()) {
					acc.OsmoBalanceBase = sdk.ZeroInt()
					acc.OsmoBalanceBonus = sdk.ZeroInt()
					acc.OsmoBalance = sdk.ZeroInt()
				}

				snapshot[address] = acc
			}

			// normalize to initial Atom supply
			noarmalizationFactor := totalAtomBalance.ToDec().Quo(totalOsmoBalance.ToDec())

			for address, acc := range snapshot {
				acc.OsmoPercent = acc.OsmoBalance.ToDec().Quo(totalOsmoBalance.ToDec())

				acc.OsmoNormalizedBalance = acc.OsmoBalance.ToDec().Mul(noarmalizationFactor).RoundInt()

				snapshot[address] = acc
			}

			// // remove empty accounts
			// finalBalances := []banktypes.Balance{}
			// totalDistr := sdk.NewInt(0)
			// for _, balance := range balances {
			// 	if balance.Coins.Empty() {
			// 		continue
			// 	}
			// 	if balance.Coins.AmountOf(osdenom).Equal(sdk.NewInt(0)) {
			// 		continue
			// 	}
			// 	finalBalances = append(finalBalances, balance)
			// 	totalDistr = totalDistr.Add(balance.Coins.AmountOf(osdenom))
			// }

			// fmt.Println("total distributed amount:", totalDistr.String())
			fmt.Printf("cosmos accounts: %d\n", len(snapshot))
			fmt.Printf("atomTotalSupply: %d\n", totalAtomBalance)
			// fmt.Printf("empty drops: %d\n", len(balances)-len(finalBalances))
			// fmt.Printf("available accounts: %d\n", len(finalBalances))

			// genAccs, err := authtypes.PackAccounts(accs)
			// if err != nil {
			// 	return fmt.Errorf("failed to convert accounts into any's: %w", err)
			// }
			// authGenState.Accounts = genAccs

			// authGenStateBz, err := cdc.MarshalJSON(&authGenState)
			// if err != nil {
			// 	return fmt.Errorf("failed to marshal auth genesis state: %w", err)
			// }

			// appState[authtypes.ModuleName] = authGenStateBz

			// bankGenState := banktypes.GetGenesisStateFromAppState(depCdc, appState)
			// bankGenState.Balances = banktypes.SanitizeGenesisBalances(balances)

			// bankGenStateBz, err := cdc.MarshalJSON(bankGenState)
			// if err != nil {
			// 	return fmt.Errorf("failed to marshal bank genesis state: %w", err)
			// }

			// appState[banktypes.ModuleName] = bankGenStateBz

			// appStateJSON, err := json.Marshal(appState)
			// if err != nil {
			// 	return fmt.Errorf("failed to marshal application genesis state: %w", err)
			// }

			// genDoc.AppState = appStateJSON

			// err = genutil.ExportGenesisFile(genDoc, genFile)
			// if err != nil {
			// 	return err
			// }

			// export snapshot directory
			snapshotJSON, err := aminoCodec.MarshalJSON(snapshot)
			if err != nil {
				return fmt.Errorf("failed to marshal snapshot: %w", err)
			}
			err = ioutil.WriteFile(snapshotOutput, snapshotJSON, 0644)
			return err
		},
	}

	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")
	cmd.Flags().String(flagSnapshotOutput, "", "Snapshot export file")
	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
