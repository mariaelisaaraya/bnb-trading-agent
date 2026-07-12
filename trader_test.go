package main

import "testing"

// Real twak 0.19 output: human-readable progress lines followed by a JSON
// receipt whose hash field is "hash", not "tx_hash".
const twakSwapOutput = `Swapping 0.004022446814969089 ETH -> 6.942460988793013781 USDT via LiquidMesh
Sending token approval...
Approval tx: https://bscscan.com/tx/0x65a88a904d59298559e93c723815999f02f8d8fa2c6186643ec5e22cd1d8296b
Swap executed!
{
  "input": "0.004022446814969089 ETH",
  "output": "6.942495566004843189 USDT",
  "provider": "LiquidMesh",
  "hash": "0xb6e803710899d0bb6122ccda0ab5f4e24c28f7e0f1f6c48ab9e19a6feb95bbe3",
  "fromChain": "bsc",
  "toChain": "bsc"
}`

func TestParseTWAKReceiptExtractsHash(t *testing.T) {
	r, err := parseTWAKReceipt(twakSwapOutput, "ETH", "sell", 6.98, 1735.26)
	if err != nil {
		t.Fatalf("parse receipt: %v", err)
	}
	want := "0xb6e803710899d0bb6122ccda0ab5f4e24c28f7e0f1f6c48ab9e19a6feb95bbe3"
	if r.TxHash != want {
		t.Fatalf("tx hash = %q, want %q", r.TxHash, want)
	}
}

func TestParseTWAKReceiptNonJSONFallback(t *testing.T) {
	raw := "Swap tx: https://bscscan.com/tx/0x685957cd1653f157c129b8b0f5572b91f697051f8abf92520135dafb8c2fbb8c\nno json here"
	r, err := parseTWAKReceipt(raw, "DOGE", "sell", 2.95, 0.0724)
	if err != nil {
		t.Fatalf("parse receipt: %v", err)
	}
	want := "0x685957cd1653f157c129b8b0f5572b91f697051f8abf92520135dafb8c2fbb8c"
	if r.TxHash != want {
		t.Fatalf("tx hash = %q, want %q", r.TxHash, want)
	}
	if r.Price != 0.0724 {
		t.Fatalf("price fallback = %v, want expectedPrice", r.Price)
	}
}
