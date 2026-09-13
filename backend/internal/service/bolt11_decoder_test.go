package service

import (
	"testing"
)

func TestClassifyDestination(t *testing.T) {
	tests := []struct {
		input       string
		expectedType DestinationType
		expectedNet  string
		isValid      bool
	}{
		{
			input:        "lnbc100u1p3xxxx",
			expectedType: DestinationBOLT11,
			expectedNet:  "mainnet",
			isValid:      true,
		},
		{
			input:        "lightning:lntb500n1p3xxxx",
			expectedType: DestinationBOLT11,
			expectedNet:  "testnet",
			isValid:      true,
		},
		{
			input:        "satoshi@blink.sv",
			expectedType: DestinationLightningAddress,
			isValid:      true,
		},
		{
			input:        "alice@getalby.com",
			expectedType: DestinationLightningAddress,
			isValid:      true,
		},
		{
			input:        "lnurl1dp68gurn8ghj7um9wfmxjcm99e3k7mf0v9cxj0m385ekvcenxc6r2c35xvukxefcv5mkvv34x5ekzd3ev56nyd3hxqurzepexejxxepnxscrvwfnv9nxzcn9xq6xyefhv53x2c35x56nyd3hxqunzd3excurzep6x5ckxefjvgcnvd3s89ex2dfcv4exqdfhx5mxzdp58y6nyv3h8yerzvfkvvenzd3svfuxyvfcvsenjvfcxc6nxvfjvsuxvd33xgunxdp38q6ryd3k8yuxv33nv36nzvpcvscrzdp5xuur2ve58qunqvfc8quxzdpjx4mxzdfkxe3rqe33xverzvmrxvcnydnx8pjnvdpnxu6k2d3nvf6xjdnx8q6xze35x9ek2d3h8q6x2dt9x56xvd3hxa6x2dfn8q6n2dp4x4mxvdrpxsuxvef5xaex2e3ev36xzen99amk2c3585cx2enx8y6n2d3h8q6x2d3j9amk2c35x56nyd3hx56n2dpnxu6n2d3nx46x2c35vcuryd3nxsenqdfexcurzwpjvg6k2wpnxcursvfhxejkyenp8q6nyvfcvqenjvfjxcerzdpjxe6nyd3jvgunvd3jxsenqdp48puxqvp5v56nvdfc8puxqdf4vg6k2wf3xgmnzvpj8q6n2vfh8puxjdm5xf3kvvnx8yexgdec8qunqvfexgmnvvpjxvmrzef3xe3kvvfc8q6nzvfc8q6x2vfkxg6xvdfcx56rvvps8q6nzdpkxg6xvvfc8qunzdp48qux2d3evsenjdpcxgmnjvfe8qun2dfexverjvp58yux2vnjvg6n2df4xgmnjdpjvg6k2wfc8qunvdpsxv6rzvfcxe6rgdpsxvmr2dpsv56nvdf4x4mxzvpnxf3kvdfnxsenjdfcvvenjvfj8qen2wpex33xyd3excurzef58yux2d338quxzer48qunsvfevserjvfcvsenjvfkxg6nywfcv3en2dpnxu6n2d3nx46x2c35vcuryd3nxsenqdfexcurzwpjvg6k2wpnxcursvfhxejkyenp8q6nyvfcvqenjvfjxcerzdpjxe6nyd3jvgunvd3jxsenqdp48puxqvp5v56nvdfc8puxqdf4vg6k2wf3xgmnzvpj8q6n2vfh8puxjdm5xf3kvvnx8yexgdec8qunqvfexgmnvvpjxvmrzef3xe3kvvfc8q6nzvfc8q6x2vfkxg6xvdfcx56rvvps8q6nzdpkxg6xvvfc8qunzdp48qux2d3evsenjdpcxgmnjvfe8qun2dfexverjvp58yux2vnjvg6n2df4xgmnjdpjvg6k2wfc8qunvdpsxv6rzvfcxe6rgdpsxvmr2dpsv56nvdf4x4mxzvpnxf3kvdfnxsenjdfcvvenjvfj8qen2wpex33xyd3excurzef58yux2d338quxzer48qunsvfevserjvfcvsenjvfkxg6nywfcv3en2d3ev46x2d3ev56nyd3hx5mnqd338pux2vfcxqux2d3j9amk2c35v56r2d3exqurzepexgunqvfcvsenqvfcxqunzdfcvven2wp48qunzdp58qunqvfcvsenqvfcxqun2dp48qux2d3evsenjdpcxgmnjvfe8qun2dfexverjvp58yux2vnjvg6n2df4xgmnjdpjvg6k2wfc8qunvdpsxv6rzvfcxe6rgdpsxvmr2dpsv56nvdf4x4mxzvpnxf3kvdfnxsenjdfcvvenjvfj8qen2wpex33xyd3excurzef58yux2d338quxzer48qunsvfevserjvfcvsenjvfkxg6nywfcv3en2",
			expectedType: DestinationLNURL,
			isValid:      true,
		},
		{
			input:        "https://domain.com/lnurl-pay?tag=payRequest",
			expectedType: DestinationLNURL,
			isValid:      true,
		},
		{
			input:        "random_garbage_data",
			expectedType: DestinationUnknown,
			isValid:      false,
		},
	}

	for _, tt := range tests {
		info := ClassifyDestination(tt.input)
		if info.Type != tt.expectedType {
			t.Errorf("ClassifyDestination(%s) type = %s; want %s", tt.input, info.Type, tt.expectedType)
		}
		if info.IsValid != tt.isValid {
			t.Errorf("ClassifyDestination(%s) isValid = %v; want %v", tt.input, info.IsValid, tt.isValid)
		}
	}
}

func TestDecodeBOLT11Amounts(t *testing.T) {
	// 100u = 100 micro-BTC = 10,000 sats
	inv1 := "lnbc100u1p3xxx"
	dec1, err := DecodeBOLT11(inv1)
	if err != nil {
		t.Fatalf("DecodeBOLT11 error: %v", err)
	}
	if dec1.AmountSats != 10000 {
		t.Errorf("dec1.AmountSats = %d; want 10000", dec1.AmountSats)
	}

	// 1m = 0.001 BTC = 100,000 sats
	inv2 := "lnbc1m1p3xxx"
	dec2, err := DecodeBOLT11(inv2)
	if err != nil {
		t.Fatalf("DecodeBOLT11 error: %v", err)
	}
	if dec2.AmountSats != 100000 {
		t.Errorf("dec2.AmountSats = %d; want 100000", dec2.AmountSats)
	}
}

func TestDecodeLNURLRaw(t *testing.T) {
	raw := "lnurlp://legend.lnbits.com/.well-known/lnurlp/test"
	url, err := DecodeLNURL(raw)
	if err != nil {
		t.Fatalf("DecodeLNURL error: %v", err)
	}
	if url != "https://legend.lnbits.com/.well-known/lnurlp/test" {
		t.Errorf("DecodeLNURL result = %s; want https://legend.lnbits.com/.well-known/lnurlp/test", url)
	}
}
