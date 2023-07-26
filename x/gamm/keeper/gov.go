package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	clmodel "github.com/osmosis-labs/osmosis/v16/x/concentrated-liquidity/model"
	"github.com/osmosis-labs/osmosis/v16/x/gamm/types"
	gammmigration "github.com/osmosis-labs/osmosis/v16/x/gamm/types/migration"
	poolmanagertypes "github.com/osmosis-labs/osmosis/v16/x/poolmanager/types"
)

func (k Keeper) HandleReplaceMigrationRecordsProposal(ctx sdk.Context, p *types.ReplaceMigrationRecordsProposal) error {
	return k.ReplaceMigrationRecords(ctx, p.Records)
}

func (k Keeper) HandleUpdateMigrationRecordsProposal(ctx sdk.Context, p *types.UpdateMigrationRecordsProposal) error {
	return k.UpdateMigrationRecords(ctx, p.Records)
}

func (k Keeper) HandleCreatingCLPoolAndLinkToCFMMProposal(ctx sdk.Context, p *types.CreateConcentratedLiquidityPoolsAndLinktoCFMMProposal) error {
	poolmanagerModuleAcc := k.accountKeeper.GetModuleAccount(ctx, poolmanagertypes.ModuleName)
	poolCreatorAddress := poolmanagerModuleAcc.GetAddress()

	for _, record := range p.PoolRecordsWithCfmmLink {
		cfmmPool, err := k.GetCFMMPool(ctx, record.BalancerPoolId)
		if err != nil {
			return err
		}

		poolLiquidity := cfmmPool.GetTotalPoolLiquidity(ctx)
		if len(poolLiquidity) != 2 {
			return fmt.Errorf("can only have 2 denoms in CL pool")
		}

		foundDenom0 := false
		denom1 := ""
		for _, coin := range poolLiquidity {
			if coin.Denom == record.Denom0 {
				foundDenom0 = true
			} else {
				denom1 = coin.Denom
			}
		}

		if !foundDenom0 {
			return fmt.Errorf("desired denom (%s) was not found in the pool", record.Denom0)
		}

		createPoolMsg := clmodel.NewMsgCreateConcentratedPool(poolCreatorAddress, record.Denom0, denom1, record.TickSpacing, record.SpreadFactor)
		concentratedPool, err := k.poolManager.CreateConcentratedPoolAsPoolManager(ctx, createPoolMsg)
		if err != nil {
			return err
		}

		// link the created cl pool with existing balancer pool
		// Set the migration link in x/gamm.
		// This will also migrate the CFMM distribution records to point to the new CL pool.
		err = k.OverwriteMigrationRecordsAndRedirectDistrRecords(ctx, gammmigration.MigrationRecords{
			BalancerToConcentratedPoolLinks: []gammmigration.BalancerToConcentratedPoolLink{
				{
					BalancerPoolId: record.BalancerPoolId,
					ClPoolId:       concentratedPool.GetId(),
				},
			},
		})
		if err != nil {
			return err
		}
	}

	return nil
}
