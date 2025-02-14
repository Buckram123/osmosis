package configurer

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	appparams "github.com/osmosis-labs/osmosis/v17/app/params"
	v16 "github.com/osmosis-labs/osmosis/v17/app/upgrades/v16"
	"github.com/osmosis-labs/osmosis/v17/tests/e2e/configurer/chain"
	"github.com/osmosis-labs/osmosis/v17/tests/e2e/configurer/config"
	"github.com/osmosis-labs/osmosis/v17/tests/e2e/containers"
	"github.com/osmosis-labs/osmosis/v17/tests/e2e/initialization"
)

type UpgradeSettings struct {
	IsEnabled  bool
	Version    string
	ForkHeight int64 // non-zero height implies that this is a fork upgrade.
}

type UpgradeConfigurer struct {
	baseConfigurer
	upgradeVersion string
	forkHeight     int64 // forkHeight > 0 implies that this is a fork upgrade. Otherwise, proposal upgrade.
}

var _ Configurer = (*UpgradeConfigurer)(nil)

func NewUpgradeConfigurer(t *testing.T, chainConfigs []*chain.Config, setupTests setupFn, containerManager *containers.Manager, upgradeVersion string, forkHeight int64) Configurer {
	t.Helper()
	return &UpgradeConfigurer{
		baseConfigurer: baseConfigurer{
			chainConfigs:     chainConfigs,
			containerManager: containerManager,
			setupTests:       setupTests,
			syncUntilHeight:  forkHeight + defaultSyncUntilHeight,
			t:                t,
		},
		forkHeight:     forkHeight,
		upgradeVersion: upgradeVersion,
	}
}

func (uc *UpgradeConfigurer) ConfigureChains() error {
	for _, chainConfig := range uc.chainConfigs {
		if err := uc.ConfigureChain(chainConfig); err != nil {
			return err
		}
	}
	return nil
}

func (uc *UpgradeConfigurer) ConfigureChain(chainConfig *chain.Config) error {
	uc.t.Logf("starting upgrade e2e infrastructure for chain-id: %s", chainConfig.Id)
	tmpDir, err := os.MkdirTemp("", "osmosis-e2e-testnet-")
	if err != nil {
		return err
	}

	validatorConfigBytes, err := json.Marshal(chainConfig.ValidatorInitConfigs)
	if err != nil {
		return err
	}

	forkHeight := uc.forkHeight
	if forkHeight > 0 {
		forkHeight = forkHeight - config.ForkHeightPreUpgradeOffset
	}

	chainInitResource, err := uc.containerManager.RunChainInitResource(chainConfig.Id, int(chainConfig.VotingPeriod), int(chainConfig.ExpeditedVotingPeriod), validatorConfigBytes, tmpDir, int(forkHeight))
	if err != nil {
		return err
	}

	fileName := fmt.Sprintf("%v/%v-encode", tmpDir, chainConfig.Id)
	uc.t.Logf("serialized init file for chain-id %v: %v", chainConfig.Id, fileName)

	// loop through the reading and unmarshaling of the init file a total of maxRetries or until error is nil
	// without this, test attempts to unmarshal file before docker container is finished writing
	var initializedChain initialization.Chain
	for i := 0; i < config.MaxRetries; i++ {
		initializedChainBytes, _ := os.ReadFile(fileName)
		err = json.Unmarshal(initializedChainBytes, &initializedChain)
		if err == nil {
			break
		}

		if i == config.MaxRetries-1 {
			if err != nil {
				return err
			}
		}

		if i > 0 {
			time.Sleep(1 * time.Second)
		}
	}
	if err := uc.containerManager.PurgeResource(chainInitResource); err != nil {
		return err
	}
	uc.initializeChainConfigFromInitChain(&initializedChain, chainConfig)
	return nil
}

func (uc *UpgradeConfigurer) CreatePreUpgradeState() error {
	// Create a WaitGroup to wait for all goroutines to complete
	var wg sync.WaitGroup
	chainA := uc.chainConfigs[0]
	chainANode, err := chainA.GetDefaultNode()
	if err != nil {
		return err
	}
	chainB := uc.chainConfigs[1]
	chainBNode, err := chainB.GetDefaultNode()
	if err != nil {
		return err
	}

	wg.Add(2)

	go func() {
		defer wg.Done()
		chainA.SendIBC(chainB, chainB.NodeConfigs[0].PublicAddress, initialization.OsmoToken)
		chainA.SendIBC(chainB, chainB.NodeConfigs[0].PublicAddress, initialization.StakeToken)
	}()

	go func() {
		defer wg.Done()
		chainB.SendIBC(chainA, chainA.NodeConfigs[0].PublicAddress, initialization.OsmoToken)
		chainB.SendIBC(chainA, chainA.NodeConfigs[0].PublicAddress, initialization.StakeToken)
	}()

	// Wait for all goroutines to complete
	wg.Wait()

	wg.Add(2)

	var daiOsmoPoolIdv16 uint64

	go func() {
		defer wg.Done()
		daiOsmoPoolIdv16 = chainANode.CreateBalancerPool("daiosmov16.json", initialization.ValidatorWalletName)
		daiOsmoShareDenom := fmt.Sprintf("gamm/pool/%d", daiOsmoPoolIdv16)
		chainANode.EnableSuperfluidAsset(chainA, daiOsmoShareDenom)
	}()

	go func() {
		defer wg.Done()
		chainBNode.CreateBalancerPool("daiosmov16.json", initialization.ValidatorWalletName)
	}()

	// Wait for all goroutines to complete
	wg.Wait()

	config.DaiOsmoPoolIdv16 = daiOsmoPoolIdv16

	var (
		poolShareDenom             string
		preUpgradePoolId           uint64
		preUpgradeStableSwapPoolId uint64
	)

	// Increment the WaitGroup counter for each goroutine
	wg.Add(4)

	go func() {
		defer wg.Done()
		preUpgradePoolId = chainANode.CreateBalancerPool("pool1A.json", initialization.ValidatorWalletName)
		poolShareDenom = fmt.Sprintf("gamm/pool/%d", preUpgradePoolId)
		chainANode.EnableSuperfluidAsset(chainA, poolShareDenom)
	}()

	go func() {
		defer wg.Done()
		chainBNode.CreateBalancerPool("pool1B.json", initialization.ValidatorWalletName)
	}()

	go func() {
		defer wg.Done()
		preUpgradeStableSwapPoolId = chainANode.CreateStableswapPool("stablePool.json", initialization.ValidatorWalletName)
	}()

	go func() {
		defer wg.Done()
		chainBNode.CreateStableswapPool("stablePool.json", initialization.ValidatorWalletName)
	}()

	// Wait for all goroutines to complete
	wg.Wait()

	config.PreUpgradePoolId = preUpgradePoolId
	config.PreUpgradeStableSwapPoolId = preUpgradeStableSwapPoolId

	fmt.Println("poolShareDenom: ", poolShareDenom)

	var (
		lockupWallet           string
		lockupWalletSuperfluid string
		stableswapWallet       string
	)

	wg.Add(3)

	go func() {
		defer wg.Done()
		// Setup wallets and send tokens to wallets (only chainA)
		lockupWallet = chainANode.CreateWalletAndFund(config.LockupWallet, []string{
			"10000000000000000000" + poolShareDenom,
		})
	}()

	go func() {
		defer wg.Done()
		lockupWalletSuperfluid = chainANode.CreateWalletAndFund(config.LockupWalletSuperfluid, []string{
			"10000000000000000000" + poolShareDenom,
		})
	}()

	go func() {
		defer wg.Done()
		stableswapWallet = chainANode.CreateWalletAndFund(config.StableswapWallet, []string{
			"100000stake",
		})
	}()

	// Wait for all goroutines to complete
	wg.Wait()

	config.LockupWallet = lockupWallet
	config.LockupWalletSuperfluid = lockupWalletSuperfluid
	config.StableswapWallet = stableswapWallet

	wg.Add(6)

	var errCh = make(chan error, 2)

	oneDai := sdk.NewCoin(v16.DAIIBCDenom, sdk.NewInt(1000000000000000000))

	go func() {
		defer wg.Done()
		// test swap exact amount in for stable swap pool (only chainA)A
		chainANode.SwapExactAmountIn("2000stake", "1", fmt.Sprintf("%d", config.PreUpgradeStableSwapPoolId), "uosmo", config.StableswapWallet)
	}()

	// Upload the rate limiting contract to both chains (as they both will be updated)
	go func() {
		defer wg.Done()
		uc.t.Logf("Uploading rate limiting contract to chainA")
		_, err := chainANode.SetupRateLimiting("", chainANode.QueryGovModuleAccount(), chainA)
		errCh <- err
	}()

	go func() {
		defer wg.Done()
		uc.t.Logf("Uploading rate limiting contract to chainB")
		_, err := chainBNode.SetupRateLimiting("", chainBNode.QueryGovModuleAccount(), chainB)
		errCh <- err
	}()

	go func() {
		defer wg.Done()
		uc.t.Logf("Lock and add to existing lock for both regular and superfluid lockups on chainA")
		chainANode.LockAndAddToExistingLock(chainA, sdk.NewInt(1000000000000000000), poolShareDenom, config.LockupWallet, config.LockupWalletSuperfluid)
	}()

	go func() {
		defer wg.Done()
		uc.t.Logf("Funding chainA's community pool with 1 DAI to be used for the upgrade")
		communityPoolFunder := chainANode.CreateWalletAndFund("communityPoolFunder", []string{
			oneDai.String(),
		})
		chainANode.FundCommunityPool(communityPoolFunder, oneDai.String())
	}()

	go func() {
		defer wg.Done()
		uc.t.Logf("Funding chainB's community pool with 1 DAI to be used for the upgrade")
		communityPoolFunder := chainBNode.CreateWalletAndFund("communityPoolFunder", []string{
			oneDai.String(),
		})
		chainBNode.FundCommunityPool(communityPoolFunder, oneDai.String())
	}()

	wg.Wait()

	close(errCh)

	for err := range errCh {
		if err != nil {
			return err
		}
	}

	return nil
}

func (uc *UpgradeConfigurer) RunSetup() error {
	return uc.setupTests(uc)
}

func (uc *UpgradeConfigurer) RunUpgrade() error {
	var err error
	if uc.forkHeight > 0 {
		err = uc.runForkUpgrade()
	} else {
		err = uc.runProposalUpgrade()
	}
	if err != nil {
		return err
	}

	// Check if the nodes are running
	for chainIndex, chainConfig := range uc.chainConfigs {
		chain := uc.baseConfigurer.GetChainConfig(chainIndex)
		for validatorIdx := range chainConfig.NodeConfigs {
			node := chain.NodeConfigs[validatorIdx]
			// Check node status
			_, err = node.Status()
			if err != nil {
				uc.t.Errorf("node is not running after upgrade, chain-id %s, node %s", chainConfig.Id, node.Name)
				return err
			}
			uc.t.Logf("node %s upgraded successfully, address %s", node.Name, node.PublicAddress)
		}
	}
	return nil
}

func (uc *UpgradeConfigurer) runProposalUpgrade() error {
	// submit, deposit, and vote for upgrade proposal
	// prop height = current height + voting period + time it takes to submit proposal + small buffer
	for _, chainConfig := range uc.chainConfigs {
		node, err := chainConfig.GetDefaultNode()
		if err != nil {
			return err
		}
		currentHeight, err := node.QueryCurrentHeight()
		if err != nil {
			return err
		}
		chainConfig.UpgradePropHeight = currentHeight + int64(chainConfig.VotingPeriod) + int64(config.PropSubmitBlocks) + int64(config.PropBufferBlocks)
		node.SubmitUpgradeProposal(uc.upgradeVersion, chainConfig.UpgradePropHeight, sdk.NewCoin(appparams.BaseCoinUnit, sdk.NewInt(config.InitialMinDeposit)))
		chainConfig.LatestProposalNumber += 1
		node.DepositProposal(chainConfig.LatestProposalNumber, false)
		propNumber := chainConfig.LatestProposalNumber

		var wg sync.WaitGroup

		for _, node := range chainConfig.NodeConfigs {
			wg.Add(1)
			go func(nodeConfig *chain.NodeConfig) {
				defer wg.Done()
				nodeConfig.VoteYesProposal(initialization.ValidatorWalletName, propNumber)
			}(node)
		}

		wg.Wait()
	}

	// wait till all chains halt at upgrade height
	for _, chainConfig := range uc.chainConfigs {
		uc.t.Logf("waiting to reach upgrade height on chain %s", chainConfig.Id)
		chainConfig.WaitUntilHeight(chainConfig.UpgradePropHeight)
		uc.t.Logf("upgrade height reached on chain %s", chainConfig.Id)
	}

	// remove all containers so we can upgrade them to the new version
	for _, chainConfig := range uc.chainConfigs {
		for _, validatorConfig := range chainConfig.NodeConfigs {
			err := uc.containerManager.RemoveNodeResource(validatorConfig.Name)
			if err != nil {
				return err
			}
		}
	}

	// remove all containers so we can upgrade them to the new version
	for _, chainConfig := range uc.chainConfigs {
		if err := uc.upgradeContainers(chainConfig, chainConfig.UpgradePropHeight); err != nil {
			return err
		}
	}
	return nil
}

func (uc *UpgradeConfigurer) runForkUpgrade() error {
	for _, chainConfig := range uc.chainConfigs {
		uc.t.Logf("waiting to reach fork height on chain %s", chainConfig.Id)
		chainConfig.WaitUntilHeight(uc.forkHeight)
		uc.t.Logf("fork height reached on chain %s", chainConfig.Id)
	}
	return nil
}

func (uc *UpgradeConfigurer) upgradeContainers(chainConfig *chain.Config, propHeight int64) error {
	// upgrade containers to the locally compiled daemon
	uc.t.Logf("starting upgrade for chain-id: %s...", chainConfig.Id)
	uc.containerManager.OsmosisRepository = containers.CurrentBranchOsmoRepository
	uc.containerManager.OsmosisTag = containers.CurrentBranchOsmoTag

	for _, node := range chainConfig.NodeConfigs {
		if err := node.Run(); err != nil {
			return err
		}
	}

	uc.t.Logf("waiting to upgrade containers on chain %s", chainConfig.Id)
	chainConfig.WaitUntilHeight(propHeight)
	uc.t.Logf("upgrade successful on chain %s", chainConfig.Id)
	return nil
}
