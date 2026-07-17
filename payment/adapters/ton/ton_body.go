package ton

import (
	"encoding/base64"

	serviceerrors "github.com/elum2b/services/errors"
	"github.com/xssnick/tonutils-go/tlb"
)

type RootTON struct {
	OpCode      uint64 `json:"op_code"`
	IHRDisabled bool   `json:"ihr_disabled"`
	Bounce      bool   `json:"bounce"`
	Bounced     bool   `json:"bounced"`
	SndrAddr    string `json:"snr_addr"`
	SrcAddr     string `json:"src_addr"`
	DstAddr     string `json:"dst_addr"`
	Amount      string `json:"amount"`
	IHRFee      string `json:"ihr_fee"`
	FwdFee      string `json:"fwd_fee"`
	CreatedLT   uint64 `json:"created_lt"`
	CreatedAt   uint32 `json:"created_at"`
	Body        Ton    `json:"body"`
}

type Ton struct {
	Message string `json:"message"`
	TxHash  string `json:"tx_hash"`
}

func (s *Sub) TonBody(ti *tlb.InternalMessage, txHash []byte) (*RootTON, error) {
	payload, err := ti.Body.BeginParse()
	if err != nil {
		return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, ErrBodyParseFailed.Message(), err)
	}

	text := ""
	if payload != nil {
		sumType, err := payload.LoadUInt(32)
		if err == nil && sumType == 0x00000000 {
			value, err := payload.LoadStringSnake()
			if err != nil {
				return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, ErrTextCommentReadFailed.Message(), err)
			}
			text = value
		} else if err != nil {
			return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, ErrSumTypeReadFailed.Message(), err)
		}
	}

	return &RootTON{
		OpCode:      0x00000000,
		IHRDisabled: ti.IHRDisabled,
		Bounce:      ti.Bounce,
		Bounced:     ti.Bounced,
		SndrAddr:    ti.SenderAddr().String(),
		SrcAddr:     ti.SrcAddr.String(),
		DstAddr:     ti.DstAddr.String(),
		Amount:      ti.Amount.Nano().String(),
		IHRFee:      ti.IHRFee.Nano().String(),
		FwdFee:      ti.FwdFee.Nano().String(),
		CreatedLT:   ti.CreatedLT,
		CreatedAt:   ti.CreatedAt,
		Body: Ton{
			Message: text,
			TxHash:  base64.StdEncoding.EncodeToString(txHash),
		},
	}, nil
}
