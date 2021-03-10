// nolint: errcheck, scopelint
package keeper

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	sdk "github.com/line/lbm-sdk/types"
	sdkErrors "github.com/line/lbm-sdk/types/errors"
	"github.com/line/lbm-sdk/x/wasm/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

func TestQueryContractState(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "wasm")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	ctx, keepers := CreateTestInput(t, false, tempDir, SupportedFeatures, nil, nil)
	accKeeper, keeper := keepers.AccountKeeper, keepers.WasmKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	topUp := sdk.NewCoins(sdk.NewInt64Coin("denom", 5000))
	creator := createFakeFundedAccount(ctx, accKeeper, deposit.Add(deposit...))
	anyAddr := createFakeFundedAccount(ctx, accKeeper, topUp)

	wasmCode, err := ioutil.ReadFile("./testdata/hackatom.wasm")
	require.NoError(t, err)

	contractID, err := keeper.Create(ctx, creator, wasmCode, "", "", nil)
	require.NoError(t, err)

	_, _, bob := keyPubAddr()
	initMsg := InitMsg{
		Verifier:    anyAddr,
		Beneficiary: bob,
	}
	initMsgBz, err := types.ModuleCdc.MarshalJSON(initMsg)
	require.NoError(t, err)

	addr, err := keeper.Instantiate(ctx, contractID, creator, nil, initMsgBz, "demo contract to query", deposit)
	require.NoError(t, err)

	contractModel := []types.Model{
		{Key: []byte("foo"), Value: []byte(`"bar"`)},
		{Key: []byte{0x0, 0x1}, Value: []byte(`{"count":8}`)},
	}
	keeper.importContractState(ctx, addr, contractModel)

	// this gets us full error, not redacted sdk.Error
	q := NewQuerier(keeper)
	specs := map[string]struct {
		srcPath []string
		srcReq  abci.RequestQuery
		// smart and raw queries (not all queries) return raw bytes from contract not []types.Model
		// if this is set, then we just compare - (should be json encoded string)
		expRes []byte
		// if success and expSmartRes is not set, we parse into []types.Model and compare (all state)
		expModelLen      int
		expModelContains []types.Model
		expErr           *sdkErrors.Error
	}{
		"query all": {
			srcPath:     []string{QueryGetContractState, addr.String(), QueryMethodContractStateAll},
			expModelLen: 3,
			expModelContains: []types.Model{
				{Key: []byte("foo"), Value: []byte(`"bar"`)},
				{Key: []byte{0x0, 0x1}, Value: []byte(`{"count":8}`)},
			},
		},
		"query raw key": {
			srcPath: []string{QueryGetContractState, addr.String(), QueryMethodContractStateRaw},
			srcReq:  abci.RequestQuery{Data: []byte("foo")},
			expRes:  []byte(`"bar"`),
		},
		"query raw binary key": {
			srcPath: []string{QueryGetContractState, addr.String(), QueryMethodContractStateRaw},
			srcReq:  abci.RequestQuery{Data: []byte{0x0, 0x1}},
			expRes:  []byte(`{"count":8}`),
		},
		"query smart": {
			srcPath: []string{QueryGetContractState, addr.String(), QueryMethodContractStateSmart},
			srcReq:  abci.RequestQuery{Data: []byte(`{"verifier":{}}`)},
			expRes:  []byte(fmt.Sprintf(`{"verifier":"%s"}`, anyAddr.String())),
		},
		"query smart invalid request": {
			srcPath: []string{QueryGetContractState, addr.String(), QueryMethodContractStateSmart},
			srcReq:  abci.RequestQuery{Data: []byte(`{"raw":{"key":"config"}}`)},
			expErr:  types.ErrQueryFailed,
		},
		"query smart with invalid json": {
			srcPath: []string{QueryGetContractState, addr.String(), QueryMethodContractStateSmart},
			srcReq:  abci.RequestQuery{Data: []byte(`not a json string`)},
			expErr:  types.ErrQueryFailed,
		},
		"query non-existent raw key": {
			srcPath: []string{QueryGetContractState, addr.String(), QueryMethodContractStateRaw},
			srcReq:  abci.RequestQuery{Data: []byte("i do not exist")},
			expRes:  nil,
		},
		"query empty raw key": {
			srcPath: []string{QueryGetContractState, addr.String(), QueryMethodContractStateRaw},
			srcReq:  abci.RequestQuery{Data: []byte("")},
			expRes:  nil,
		},
		"query nil raw key": {
			srcPath: []string{QueryGetContractState, addr.String(), QueryMethodContractStateRaw},
			srcReq:  abci.RequestQuery{Data: nil},
			expRes:  nil,
		},
		"query raw with unknown address": {
			srcPath: []string{QueryGetContractState, anyAddr.String(), QueryMethodContractStateRaw},
			expRes:  nil,
		},
		"query all with unknown address": {
			srcPath:     []string{QueryGetContractState, anyAddr.String(), QueryMethodContractStateAll},
			expModelLen: 0,
		},
		"query smart with unknown address": {
			srcPath:     []string{QueryGetContractState, anyAddr.String(), QueryMethodContractStateSmart},
			expModelLen: 0,
			expErr:      types.ErrNotFound,
		},
	}

	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			binResult, err := q(ctx, spec.srcPath, spec.srcReq)
			// require.True(t, spec.expErr.Is(err), "unexpected error")
			require.True(t, spec.expErr.Is(err), err)

			// if smart query, check custom response
			if spec.srcPath[2] != QueryMethodContractStateAll {
				require.Equal(t, spec.expRes, binResult)
				return
			}

			// otherwise, check returned models
			var r []types.Model
			if spec.expErr == nil {
				require.NoError(t, types.ModuleCdc.UnmarshalJSON(binResult, &r))
				if spec.expModelLen == 0 {
					require.Nil(t, r)
				} else {
					require.NotNil(t, r)
				}
			}
			require.Len(t, r, spec.expModelLen)
			// and in result set
			for _, v := range spec.expModelContains {
				assert.Contains(t, r, v)
			}
		})
	}
}

func TestListContractByCodeOrdering(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "wasm")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	ctx, keepers := CreateTestInput(t, false, tempDir, SupportedFeatures, nil, nil)
	accKeeper, keeper := keepers.AccountKeeper, keepers.WasmKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 1000000))
	topUp := sdk.NewCoins(sdk.NewInt64Coin("denom", 500))
	creator := createFakeFundedAccount(ctx, accKeeper, deposit)
	anyAddr := createFakeFundedAccount(ctx, accKeeper, topUp)

	wasmCode, err := ioutil.ReadFile("./testdata/hackatom.wasm")
	require.NoError(t, err)

	codeID, err := keeper.Create(ctx, creator, wasmCode, "", "", nil)
	require.NoError(t, err)

	_, _, bob := keyPubAddr()
	initMsg := InitMsg{
		Verifier:    anyAddr,
		Beneficiary: bob,
	}
	initMsgBz, err := types.ModuleCdc.MarshalJSON(initMsg)
	require.NoError(t, err)

	// manage some realistic block settings
	var h int64 = 10
	setBlock := func(ctx sdk.Context, height int64) sdk.Context {
		ctx = ctx.WithBlockHeight(height)
		meter := sdk.NewGasMeter(1000000)
		ctx = ctx.WithGasMeter(meter)
		ctx = ctx.WithBlockGasMeter(meter)
		return ctx
	}

	// create 10 contracts with real block/gas setup
	for i := range [10]int{} {
		// 3 tx per block, so we ensure both comparisons work
		if i%3 == 0 {
			ctx = setBlock(ctx, h)
			h++
		}
		_, err = keeper.Instantiate(ctx, codeID, creator, nil, initMsgBz, fmt.Sprintf("contract %d", i), topUp)
		require.NoError(t, err)
	}

	// query and check the results are properly sorted
	q := NewQuerier(keeper)
	query := []string{QueryListContractByCode, fmt.Sprintf("%d", codeID)}
	data := abci.RequestQuery{}
	res, err := q(ctx, query, data)
	require.NoError(t, err)

	var contracts []types.ContractInfoResponse
	err = types.ModuleCdc.UnmarshalJSON(res, &contracts)
	require.NoError(t, err)

	require.Equal(t, 10, len(contracts))

	for i, contract := range contracts {
		assert.Equal(t, fmt.Sprintf("contract %d", i), contract.GetLabel())
		assert.NotEmpty(t, contract.GetAddress())
	}
	assert.NotContains(t, string(res), "create")
	assert.NotContains(t, string(res), "Create")
}

func TestQueryContractHistory(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "wasm")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	ctx, keepers := CreateTestInput(t, false, tempDir, SupportedFeatures, nil, nil)
	keeper := keepers.WasmKeeper

	var (
		otherAddr sdk.AccAddress = bytes.Repeat([]byte{0x2}, sdk.AddrLen)
	)

	specs := map[string]struct {
		srcQueryAddr sdk.AccAddress
		srcHistory   []types.ContractCodeHistoryEntry
		expContent   []types.ContractCodeHistoryEntry
	}{
		"response with internal fields cleared": {
			srcHistory: []types.ContractCodeHistoryEntry{{
				Operation: types.GenesisContractCodeHistoryType,
				CodeID:    firstCodeID,
				Updated:   types.NewAbsoluteTxPosition(ctx),
				Msg:       []byte(`"init message"`),
			}},
			expContent: []types.ContractCodeHistoryEntry{{
				Operation: types.GenesisContractCodeHistoryType,
				CodeID:    firstCodeID,
				Msg:       []byte(`"init message"`),
			}},
		},
		"response with multiple entries": {
			srcHistory: []types.ContractCodeHistoryEntry{{
				Operation: types.InitContractCodeHistoryType,
				CodeID:    firstCodeID,
				Updated:   types.NewAbsoluteTxPosition(ctx),
				Msg:       []byte(`"init message"`),
			}, {
				Operation: types.MigrateContractCodeHistoryType,
				CodeID:    2,
				Updated:   types.NewAbsoluteTxPosition(ctx),
				Msg:       []byte(`"migrate message 1"`),
			}, {
				Operation: types.MigrateContractCodeHistoryType,
				CodeID:    3,
				Updated:   types.NewAbsoluteTxPosition(ctx),
				Msg:       []byte(`"migrate message 2"`),
			}},
			expContent: []types.ContractCodeHistoryEntry{{
				Operation: types.InitContractCodeHistoryType,
				CodeID:    firstCodeID,
				Msg:       []byte(`"init message"`),
			}, {
				Operation: types.MigrateContractCodeHistoryType,
				CodeID:    2,
				Msg:       []byte(`"migrate message 1"`),
			}, {
				Operation: types.MigrateContractCodeHistoryType,
				CodeID:    3,
				Msg:       []byte(`"migrate message 2"`),
			}},
		},
		"unknown contract address": {
			srcQueryAddr: otherAddr,
			srcHistory: []types.ContractCodeHistoryEntry{{
				Operation: types.GenesisContractCodeHistoryType,
				CodeID:    firstCodeID,
				Updated:   types.NewAbsoluteTxPosition(ctx),
				Msg:       []byte(`"init message"`),
			}},
			expContent: nil,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			_, _, myContractAddr := keyPubAddr()
			keeper.appendToContractHistory(ctx, myContractAddr, spec.srcHistory...)
			q := NewQuerier(keeper)
			queryContractAddr := spec.srcQueryAddr
			if queryContractAddr == nil {
				queryContractAddr = myContractAddr
			}

			// when
			query := []string{QueryContractHistory, queryContractAddr.String()}
			data := abci.RequestQuery{}
			resData, err := q(ctx, query, data)

			// then
			require.NoError(t, err)
			if spec.expContent == nil {
				require.Nil(t, resData)
				return
			}
			var got []types.ContractHistoryResponse
			err = types.ModuleCdc.UnmarshalJSON(resData, &got)
			require.NoError(t, err)

			assertContractHistory(t, spec.expContent, got)
		})
	}
}

func assertContractHistory(t *testing.T, expected []types.ContractCodeHistoryEntry, actual []types.ContractHistoryResponse) {
	assert.Equal(t, len(expected), len(actual))

	for i, entry := range expected {
		expectedResponse := types.NewContractHistoryResponse(entry)
		assert.Equal(t, expectedResponse, actual[i])
	}
}

func TestQueryCodeList(t *testing.T) {
	wasmCode, err := ioutil.ReadFile("./testdata/hackatom.wasm")
	require.NoError(t, err)

	specs := map[string]struct {
		codeIDs []uint64
	}{
		"none": {},
		"no gaps": {
			codeIDs: []uint64{1, 2, 3},
		},
		"with gaps": {
			codeIDs: []uint64{2, 4, 6},
		},
	}

	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			tempDir, err := ioutil.TempDir("", "wasm")
			require.NoError(t, err)
			defer os.RemoveAll(tempDir)
			ctx, keepers := CreateTestInput(t, false, tempDir, SupportedFeatures, nil, nil)
			keeper := keepers.WasmKeeper

			for _, codeID := range spec.codeIDs {
				require.NoError(t, keeper.importCode(ctx, codeID,
					types.CodeInfoFixture(types.WithSHA256CodeHash(wasmCode)),
					wasmCode),
				)
			}
			q := NewQuerier(keeper)
			// when
			query := []string{QueryListCode}
			data := abci.RequestQuery{}
			resData, err := q(ctx, query, data)

			// then
			require.NoError(t, err)

			var got []types.CodeInfoResponse
			err = types.ModuleCdc.UnmarshalJSON(resData, &got)
			require.NoError(t, err)
			require.Len(t, got, len(spec.codeIDs))
			for i, exp := range spec.codeIDs {
				assert.EqualValues(t, exp, got[i].GetID())
			}
		})
	}
}
