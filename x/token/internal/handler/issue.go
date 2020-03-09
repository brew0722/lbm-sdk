package handler

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/line/link/x/token/internal/keeper"
	"github.com/line/link/x/token/internal/types"
)

func handleMsgIssue(ctx sdk.Context, keeper keeper.Keeper, msg types.MsgIssue) sdk.Result {
	token := types.NewToken(keeper.NewContractID(ctx), msg.Name, msg.Symbol, msg.Meta, msg.ImageURI, msg.Decimals, msg.Mintable)
	err := keeper.IssueToken(ctx, token, msg.Amount, msg.Owner, msg.To)
	if err != nil {
		return err.Result()
	}

	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.AttributeValueCategory),
			sdk.NewAttribute(sdk.AttributeKeySender, msg.Owner.String()),
		),
	})
	return sdk.Result{Events: ctx.EventManager().Events()}
}
