package stableswap

import (
	fmt "fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/osmosis-labs/osmosis/v10/app/apptesting/osmoassert"
)

func TestCFMMInvariantTwoAssets(t *testing.T) {
	kErrTolerance := sdk.OneDec()

	tests := []struct {
		xReserve sdk.Dec
		yReserve sdk.Dec
		yIn      sdk.Dec
	}{
		{
			sdk.NewDec(100),
			sdk.NewDec(100),
			sdk.NewDec(1),
		},
		{
			sdk.NewDec(100),
			sdk.NewDec(100),
			sdk.NewDec(1000),
		},
		// {
		// 	sdk.NewDec(100000),
		// 	sdk.NewDec(100000),
		// 	sdk.NewDec(10000),
		// },
	}

	for _, test := range tests {
		// using two-asset cfmm
		k0 := cfmmConstant(test.xReserve, test.yReserve)
		xOut := solveCfmm(test.xReserve, test.yReserve, test.yIn)

		k1 := cfmmConstant(test.xReserve.Sub(xOut), test.yReserve.Add(test.yIn))
		osmoassert.DecApproxEq(t, k0, k1, kErrTolerance)

		// using multi-asset cfmm (should be equivalent with u = 1, w = 0)
		k2 := cfmmConstantMulti(test.xReserve, test.yReserve, sdk.OneDec(), sdk.ZeroDec())
		osmoassert.DecApproxEq(t, k2, k0, kErrTolerance)
		xOut2 := solveCfmmMulti(test.xReserve, test.yReserve, sdk.ZeroDec(), test.yIn)
		fmt.Println(xOut2)
		k3 := cfmmConstantMulti(test.xReserve.Sub(xOut2), test.yReserve.Add(test.yIn), sdk.OneDec(), sdk.ZeroDec())
		osmoassert.DecApproxEq(t, k2, k3, kErrTolerance)
	}
}

func TestCFMMInvariantMultiAssets(t *testing.T) {
	kErrTolerance := sdk.OneDec()

	tests := []struct {
		xReserve    sdk.Dec
		yReserve    sdk.Dec
		uReserve    sdk.Dec
		wSumSquares sdk.Dec
		yIn         sdk.Dec
	}{
		{
			sdk.NewDec(100),
			sdk.NewDec(100),
			// represents a 4-asset pool with 100 in each reserve
			sdk.NewDec(200),
			sdk.NewDec(20000),
			sdk.NewDec(1),
		},
		{
			sdk.NewDec(100),
			sdk.NewDec(100),
			sdk.NewDec(200),
			sdk.NewDec(20000),
			sdk.NewDec(1000),
		},
		// {
		// 	sdk.NewDec(100000),
		// 	sdk.NewDec(100000),
		// 	sdk.NewDec(10000),
		// },
	}

	for _, test := range tests {
		// using multi-asset cfmm
		k2 := cfmmConstantMulti(test.xReserve, test.yReserve, test.uReserve, test.wSumSquares)
		xOut2 := solveCfmmMulti(test.xReserve, test.yReserve, test.wSumSquares, test.yIn)
		fmt.Println(xOut2)
		k3 := cfmmConstantMulti(test.xReserve.Sub(xOut2), test.yReserve.Add(test.yIn), test.uReserve, test.wSumSquares)
		osmoassert.DecApproxEq(t, k2, k3, kErrTolerance)
	}
}
