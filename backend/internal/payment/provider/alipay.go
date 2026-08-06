package provider

import (
	"context"
	"crypto/md5"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/smartwalle/alipay/v3"
)

// Alipay product codes.
const (
	alipayProductCodePreCreate = "FACE_TO_FACE_PAYMENT"
	alipayProductCodeWapPay    = "QUICK_WAP_WAY"
	alipayProductCodePagePay   = "FAST_INSTANT_TRADE_PAY"
)

// Alipay response constants.
const (
	alipayFundChangeYes    = "Y"
	alipayErrTradeNotExist = "ACQ.TRADE_NOT_EXIST"
	alipayRefundSuffix     = "-refund"
)

var (
	alipayTradeWapPay = func(client *alipay.Client, param alipay.TradeWapPay) (*url.URL, error) {
		return client.TradeWapPay(param)
	}
	alipayTradePreCreate = func(ctx context.Context, client *alipay.Client, param alipay.TradePreCreate) (*alipay.TradePreCreateRsp, error) {
		return client.TradePreCreate(ctx, param)
	}
	alipayTradePagePay = func(client *alipay.Client, param alipay.TradePagePay) (*url.URL, error) {
		return client.TradePagePay(param)
	}
)

type Alipay struct {
	instanceID string
	config     map[string]string // appId, privateKey, publicKey/certificate fields, notifyUrl, returnUrl

	mu     sync.Mutex
	client *alipay.Client
}

const (
	alipayDefaultProductionGateway = "https://openapi.alipay.com/gateway.do"
	alipayDefaultSandboxGateway    = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"
)

// NewAlipay creates a new Alipay provider instance.
func NewAlipay(instanceID string, config map[string]string) (*Alipay, error) {
	normalized, err := normalizeAlipayConfig(config)
	if err != nil {
		return nil, err
	}
	return &Alipay{
		instanceID: instanceID,
		config:     normalized,
	}, nil
}

func (a *Alipay) getClient() (*alipay.Client, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client != nil {
		return a.client, nil
	}
	client, err := newAlipayClient(a.config)
	if err != nil {
		return nil, err
	}
	a.client = client
	return client, nil
}

func normalizeAlipayConfig(raw map[string]string) (map[string]string, error) {
	config := make(map[string]string, len(raw)+8)
	for key, value := range raw {
		config[key] = value
	}

	for _, key := range []string{"appId", "privateKey"} {
		if strings.TrimSpace(config[key]) == "" {
			return nil, fmt.Errorf("alipay config missing required key: %s", key)
		}
	}
	config["appId"] = strings.TrimSpace(config["appId"])

	privateKey, err := loadAlipayConfigValue(config, "privateKey", "privateKeyPath")
	if err != nil {
		return nil, err
	}
	config["privateKey"] = privateKey

	signType := strings.ToUpper(strings.TrimSpace(config["signType"]))
	if signType == "" {
		signType = "RSA2"
	}
	if signType != "RSA2" {
		return nil, fmt.Errorf("alipay config signType must be RSA2, got %q", signType)
	}
	config["signType"] = signType

	publicKey, err := loadAlipayConfigValue(config, "publicKey", "publicKeyPath")
	if err != nil {
		return nil, err
	}
	legacyPublicKey, err := loadAlipayConfigValue(config, "alipayPublicKey", "alipayPublicKeyPath")
	if err != nil {
		return nil, err
	}
	if publicKey == "" {
		publicKey = legacyPublicKey
	}
	config["publicKey"] = publicKey
	config["alipayPublicKey"] = legacyPublicKey

	certificateFields := []struct {
		key     string
		pathKey string
	}{
		{key: "appCertContent", pathKey: "appCertPath"},
		{key: "alipayPublicCertContent", pathKey: "alipayPublicCertPath"},
		{key: "rootCertContent", pathKey: "rootCertPath"},
	}
	certificateConfigured := false
	for _, field := range certificateFields {
		value, err := loadAlipayConfigValue(config, field.key, field.pathKey)
		if err != nil {
			return nil, err
		}
		config[field.key] = value
		certificateConfigured = certificateConfigured || value != ""
	}
	if certificateConfigured {
		if publicKey != "" {
			return nil, fmt.Errorf("alipay config certificate mode cannot be combined with publicKey")
		}
		for _, field := range certificateFields {
			if strings.TrimSpace(config[field.key]) == "" {
				return nil, fmt.Errorf("alipay config certificate mode requires %s", field.key)
			}
		}
		if err := validateAlipayCertificates(config); err != nil {
			return nil, err
		}
	} else if publicKey == "" {
		// Keep legacy deferred validation semantics: provider instances may be
		// stored as drafts and merchant identity checks may run before a payment
		// client is needed. The SDK client validates the key material on first use.
		config["publicKey"] = ""
	}

	if encryptKey := strings.TrimSpace(config["encryptKey"]); encryptKey != "" {
		decoded, err := base64.StdEncoding.DecodeString(encryptKey)
		if err != nil {
			return nil, fmt.Errorf("alipay config encryptKey must be base64: %w", err)
		}
		if len(decoded) != 16 && len(decoded) != 24 && len(decoded) != 32 {
			return nil, fmt.Errorf("alipay config encryptKey must decode to 16, 24, or 32 bytes, got %d", len(decoded))
		}
		config["encryptKey"] = encryptKey
	}

	if _, _, err := resolveAlipayGateway(config); err != nil {
		return nil, err
	}
	return config, nil
}

func loadAlipayConfigValue(config map[string]string, key, pathKey string) (string, error) {
	value := strings.TrimSpace(config[key])
	path := strings.TrimSpace(config[pathKey])
	if value != "" && path != "" {
		return "", fmt.Errorf("alipay config %s and %s cannot both be set", key, pathKey)
	}
	if path == "" {
		return value, nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("alipay config %s: read %q: %w", pathKey, path, err)
	}
	value = strings.TrimSpace(string(contents))
	if value == "" {
		return "", fmt.Errorf("alipay config %s points to an empty file", pathKey)
	}
	return value, nil
}

func resolveAlipayGateway(config map[string]string) (string, bool, error) {
	// customGateway is an explicit override. gatewayUrl remains the frontend
	// template field and wins over environment defaults when it is present.
	gateway := strings.TrimSpace(config["customGateway"])
	if gateway == "" {
		gateway = strings.TrimSpace(config["gatewayUrl"])
	}
	if gateway == "" {
		switch strings.ToLower(strings.TrimSpace(config["environment"])) {
		case "", "production", "prod":
			gateway = alipayDefaultProductionGateway
		case "sandbox", "test":
			gateway = alipayDefaultSandboxGateway
		default:
			return "", false, fmt.Errorf("alipay config environment must be production or sandbox, got %q", config["environment"])
		}
	}

	parsed, err := url.Parse(gateway)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false, fmt.Errorf("alipay config gatewayUrl must be an absolute http(s) URL without query or fragment, got %q", gateway)
	}
	production := !isAlipaySandboxGateway(parsed)
	return gateway, production, nil
}

func isAlipaySandboxGateway(gateway *url.URL) bool {
	host := strings.ToLower(gateway.Hostname())
	return strings.Contains(host, "sandbox") || strings.Contains(host, "alipaydev")
}

func newAlipayClient(config map[string]string) (*alipay.Client, error) {
	gateway, production, err := resolveAlipayGateway(config)
	if err != nil {
		return nil, err
	}
	var opts []alipay.OptionFunc
	if production {
		opts = append(opts, alipay.WithProductionGateway(gateway))
	} else {
		opts = append(opts, alipay.WithSandboxGateway(gateway))
	}
	client, err := alipay.New(config["appId"], config["privateKey"], production, opts...)
	if err != nil {
		return nil, fmt.Errorf("alipay config privateKey is invalid: %w", err)
	}

	if config["appCertContent"] != "" {
		if err := client.LoadAppCertPublicKey(config["appCertContent"]); err != nil {
			return nil, fmt.Errorf("alipay config appCertContent is invalid: %w", err)
		}
		if err := client.LoadAliPayRootCert(config["rootCertContent"]); err != nil {
			return nil, fmt.Errorf("alipay config rootCertContent is invalid: %w", err)
		}
		if err := client.LoadAlipayCertPublicKey(config["alipayPublicCertContent"]); err != nil {
			return nil, fmt.Errorf("alipay config alipayPublicCertContent is invalid: %w", err)
		}
	} else if err := client.LoadAliPayPublicKey(config["publicKey"]); err != nil {
		return nil, fmt.Errorf("alipay config publicKey is invalid: %w", err)
	}

	if encryptKey := strings.TrimSpace(config["encryptKey"]); encryptKey != "" {
		if err := client.SetEncryptKey(encryptKey); err != nil {
			return nil, fmt.Errorf("alipay config encryptKey is invalid: %w", err)
		}
	}
	return client, nil
}

func validateAlipayCertificates(config map[string]string) error {
	appCerts, err := parseAlipayCertificates(config["appCertContent"], "appCertContent")
	if err != nil {
		return err
	}
	if len(appCerts) != 1 || !isRSAAlipayCertificate(appCerts[0]) {
		return fmt.Errorf("alipay config appCertContent must contain one RSA certificate")
	}

	publicCerts, err := parseAlipayCertificates(config["alipayPublicCertContent"], "alipayPublicCertContent")
	if err != nil {
		return err
	}
	if len(publicCerts) != 1 || !isRSAAlipayCertificate(publicCerts[0]) {
		return fmt.Errorf("alipay config alipayPublicCertContent must contain one RSA certificate")
	}

	rootCerts, err := parseAlipayCertificates(config["rootCertContent"], "rootCertContent")
	if err != nil {
		return err
	}
	rootSerials := make([]string, 0, len(rootCerts))
	for _, cert := range rootCerts {
		if !isRSAAlipayCertificate(cert) || (cert.SignatureAlgorithm != x509.SHA256WithRSA && cert.SignatureAlgorithm != x509.SHA1WithRSA) {
			continue
		}
		rootSerials = append(rootSerials, alipayCertificateSN(cert))
	}
	if len(rootSerials) == 0 {
		return fmt.Errorf("alipay config rootCertContent must contain an RSA root certificate")
	}

	if expected := strings.TrimSpace(config["appCertSn"]); expected != "" {
		actual := alipayCertificateSN(appCerts[0])
		if expected != actual {
			return fmt.Errorf("alipay config appCertSn %q does not match appCertContent serial %q", expected, actual)
		}
	}
	if expected := strings.TrimSpace(config["alipayRootCertSn"]); expected != "" {
		actual := strings.Join(rootSerials, "_")
		if expected != actual {
			return fmt.Errorf("alipay config alipayRootCertSn %q does not match rootCertContent serial %q", expected, actual)
		}
	}
	return nil
}

func parseAlipayCertificates(raw, field string) ([]*x509.Certificate, error) {
	remaining := []byte(raw)
	certs := make([]*x509.Certificate, 0, 1)
	for len(remaining) > 0 {
		block, rest := pem.Decode(remaining)
		if block == nil {
			if len(certs) == 0 {
				return nil, fmt.Errorf("alipay config %s must contain PEM certificate data", field)
			}
			if strings.TrimSpace(string(remaining)) != "" {
				return nil, fmt.Errorf("alipay config %s contains invalid trailing data", field)
			}
			break
		}
		if block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("alipay config %s contains PEM block %q, expected CERTIFICATE", field, block.Type)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("alipay config %s contains invalid certificate: %w", field, err)
		}
		certs = append(certs, cert)
		remaining = rest
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("alipay config %s must contain a certificate", field)
	}
	return certs, nil
}

func isRSAAlipayCertificate(cert *x509.Certificate) bool {
	_, ok := cert.PublicKey.(*rsa.PublicKey)
	return ok
}

// This matches smartwalle/alipay's certificate serial calculation.
func alipayCertificateSN(cert *x509.Certificate) string {
	digest := md5.Sum([]byte(cert.Issuer.String() + cert.SerialNumber.String()))
	return hex.EncodeToString(digest[:])
}

func (a *Alipay) Name() string        { return "Alipay" }
func (a *Alipay) ProviderKey() string { return payment.TypeAlipay }
func (a *Alipay) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeAlipay}
}

func (a *Alipay) MerchantIdentityMetadata() map[string]string {
	if a == nil {
		return nil
	}
	appID := strings.TrimSpace(a.config["appId"])
	if appID == "" {
		return nil
	}
	return map[string]string{"app_id": appID}
}

// CreatePayment creates an Alipay payment using the following routing:
//   - Mobile (H5), default: alipay.trade.wap.pay — browser redirect into Alipay.
//   - Mobile with AlipayMobilePrecreate: alipay.trade.precreate — return the
//     dynamic QR payload so the frontend can open it through the Alipay app.
//   - Desktop, default: prefer alipay.trade.precreate (FACE_TO_FACE_PAYMENT) to
//     get a scannable QR payload. If precreate is unavailable for the merchant,
//     fall back to alipay.trade.page.pay and expose pay_url only — the frontend
//     opens the Alipay checkout in a new tab.
//   - Desktop, paymentMode == "redirect": skip precreate and go straight to
//     alipay.trade.page.pay so the frontend always opens the Alipay checkout
//     in a new tab. Use this when the merchant has not enabled FACE_TO_FACE_PAYMENT.
//
// Note: alipay.trade.page.pay returns a checkout page URL, not a scannable
// payment QR. Never expose it via the QRCode field.
func (a *Alipay) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	client, err := a.getClient()
	if err != nil {
		return nil, err
	}

	notifyURL := a.config["notifyUrl"]
	if req.NotifyURL != "" {
		notifyURL = req.NotifyURL
	}
	returnURL := a.config["returnUrl"]
	if req.ReturnURL != "" {
		returnURL = req.ReturnURL
	}

	if req.IsMobile {
		if req.AlipayMobilePrecreate {
			return a.createPrecreateTrade(ctx, client, req, notifyURL)
		}
		return a.createWapTrade(client, req, notifyURL, returnURL)
	}
	return a.createDesktopTrade(ctx, client, req, notifyURL, returnURL)
}

func (a *Alipay) createWapTrade(client *alipay.Client, req payment.CreatePaymentRequest, notifyURL, returnURL string) (*payment.CreatePaymentResponse, error) {
	param := alipay.TradeWapPay{}
	param.OutTradeNo = req.OrderID
	param.TotalAmount = req.Amount
	param.Subject = req.Subject
	param.ProductCode = alipayProductCodeWapPay
	param.NotifyURL = notifyURL
	param.ReturnURL = returnURL

	payURL, err := alipayTradeWapPay(client, param)
	if err != nil {
		return nil, fmt.Errorf("alipay TradeWapPay: %w", err)
	}
	return &payment.CreatePaymentResponse{
		TradeNo: req.OrderID,
		PayURL:  payURL.String(),
	}, nil
}

func (a *Alipay) createDesktopTrade(ctx context.Context, client *alipay.Client, req payment.CreatePaymentRequest, notifyURL, returnURL string) (*payment.CreatePaymentResponse, error) {
	// Explicit redirect mode: merchant opted into "always open the Alipay
	// checkout page in a new tab" via the provider instance's payment_mode.
	// Skip precreate to avoid a wasted API call.
	if strings.EqualFold(strings.TrimSpace(a.config["paymentMode"]), "redirect") {
		return a.createPagePayTrade(client, req, notifyURL, returnURL)
	}

	resp, precreateErr := a.createPrecreateTrade(ctx, client, req, notifyURL)
	if precreateErr == nil {
		return resp, nil
	}

	resp, pagePayErr := a.createPagePayTrade(client, req, notifyURL, returnURL)
	if pagePayErr == nil {
		return resp, nil
	}

	return nil, fmt.Errorf("alipay desktop payment failed: precreate=%v; pagepay=%w", precreateErr, pagePayErr)
}

func (a *Alipay) createPrecreateTrade(ctx context.Context, client *alipay.Client, req payment.CreatePaymentRequest, notifyURL string) (*payment.CreatePaymentResponse, error) {
	param := alipay.TradePreCreate{}
	param.OutTradeNo = req.OrderID
	param.TotalAmount = req.Amount
	param.Subject = req.Subject
	param.ProductCode = alipayProductCodePreCreate
	param.NotifyURL = notifyURL

	rsp, err := alipayTradePreCreate(ctx, client, param)
	if err != nil {
		return nil, fmt.Errorf("alipay TradePreCreate: %w", err)
	}
	if rsp == nil {
		return nil, fmt.Errorf("alipay TradePreCreate: empty response")
	}
	if rsp.IsFailure() {
		return nil, fmt.Errorf("alipay TradePreCreate failed: %s", rsp.Error.Error())
	}
	if strings.TrimSpace(rsp.QRCode) == "" {
		return nil, fmt.Errorf("alipay TradePreCreate: empty qr_code")
	}

	return &payment.CreatePaymentResponse{
		TradeNo: req.OrderID,
		QRCode:  rsp.QRCode,
	}, nil
}

func (a *Alipay) createPagePayTrade(client *alipay.Client, req payment.CreatePaymentRequest, notifyURL, returnURL string) (*payment.CreatePaymentResponse, error) {
	param := alipay.TradePagePay{}
	param.OutTradeNo = req.OrderID
	param.TotalAmount = req.Amount
	param.Subject = req.Subject
	param.ProductCode = alipayProductCodePagePay
	param.NotifyURL = notifyURL
	param.ReturnURL = returnURL

	payURL, err := alipayTradePagePay(client, param)
	if err != nil {
		return nil, fmt.Errorf("alipay TradePagePay: %w", err)
	}
	// Only PayURL is exposed: alipay.trade.page.pay returns a checkout page URL
	// that must be opened in a browser, not a scannable payment QR. Setting it
	// as QRCode would let the frontend render an unscannable image.
	return &payment.CreatePaymentResponse{
		TradeNo: req.OrderID,
		PayURL:  payURL.String(),
	}, nil
}

// QueryOrder queries the trade status via Alipay.
func (a *Alipay) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	client, err := a.getClient()
	if err != nil {
		return nil, err
	}

	result, err := client.TradeQuery(ctx, alipay.TradeQuery{OutTradeNo: tradeNo})
	if err != nil {
		if isTradeNotExist(err) {
			return &payment.QueryOrderResponse{
				TradeNo: tradeNo,
				Status:  payment.ProviderStatusPending,
			}, nil
		}
		return nil, fmt.Errorf("alipay TradeQuery: %w", err)
	}

	status := payment.ProviderStatusPending
	switch result.TradeStatus {
	case alipay.TradeStatusSuccess, alipay.TradeStatusFinished:
		status = payment.ProviderStatusPaid
	case alipay.TradeStatusClosed:
		status = payment.ProviderStatusFailed
	}

	amount, err := strconv.ParseFloat(result.TotalAmount, 64)
	if err != nil {
		amount, err = parseAlipayAmount(
			result.TotalAmount,
			result.ReceiptAmount,
			result.BuyerPayAmount,
			result.InvoiceAmount,
		)
		if err != nil {
			return nil, fmt.Errorf("alipay parse amount: %w", err)
		}
	}

	return &payment.QueryOrderResponse{
		TradeNo:  result.TradeNo,
		Status:   status,
		Amount:   amount,
		PaidAt:   result.SendPayDate,
		Metadata: a.MerchantIdentityMetadata(),
	}, nil
}

// VerifyNotification decodes and verifies an Alipay async notification.
func (a *Alipay) VerifyNotification(ctx context.Context, rawBody string, _ map[string]string) (*payment.PaymentNotification, error) {
	client, err := a.getClient()
	if err != nil {
		return nil, err
	}

	values, err := url.ParseQuery(rawBody)
	if err != nil {
		return nil, fmt.Errorf("alipay parse notification: %w", err)
	}

	notification, err := client.DecodeNotification(ctx, values)
	if err != nil {
		return nil, fmt.Errorf("alipay verify notification: %w", err)
	}

	status := payment.ProviderStatusFailed
	if notification.TradeStatus == alipay.TradeStatusSuccess || notification.TradeStatus == alipay.TradeStatusFinished {
		status = payment.ProviderStatusSuccess
	}

	amount, err := strconv.ParseFloat(notification.TotalAmount, 64)
	if err != nil {
		amount, err = parseAlipayAmount(
			notification.TotalAmount,
			notification.ReceiptAmount,
			notification.BuyerPayAmount,
		)
		if err != nil {
			return nil, fmt.Errorf("alipay parse notification amount: %w", err)
		}
	}

	metadata := a.MerchantIdentityMetadata()
	if appID := strings.TrimSpace(notification.AppId); appID != "" {
		if metadata == nil {
			metadata = map[string]string{}
		}
		metadata["app_id"] = appID
	}

	return &payment.PaymentNotification{
		TradeNo:  notification.TradeNo,
		OrderID:  notification.OutTradeNo,
		Amount:   amount,
		Status:   status,
		RawData:  rawBody,
		Metadata: metadata,
	}, nil
}

// Refund requests a refund through Alipay.
func (a *Alipay) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	client, err := a.getClient()
	if err != nil {
		return nil, err
	}

	result, err := client.TradeRefund(ctx, alipay.TradeRefund{
		OutTradeNo:   req.OrderID,
		RefundAmount: req.Amount,
		RefundReason: req.Reason,
		OutRequestNo: fmt.Sprintf("%s-refund-%d", req.OrderID, time.Now().UnixNano()),
	})
	if err != nil {
		return nil, fmt.Errorf("alipay TradeRefund: %w", err)
	}

	refundStatus := payment.ProviderStatusPending
	if result.FundChange == alipayFundChangeYes {
		refundStatus = payment.ProviderStatusSuccess
	}

	refundID := result.TradeNo
	if refundID == "" {
		refundID = req.OrderID + alipayRefundSuffix
	}

	return &payment.RefundResponse{
		RefundID: refundID,
		Status:   refundStatus,
	}, nil
}

// CancelPayment closes a pending trade on Alipay.
func (a *Alipay) CancelPayment(ctx context.Context, tradeNo string) error {
	client, err := a.getClient()
	if err != nil {
		return err
	}

	_, err = client.TradeClose(ctx, alipay.TradeClose{OutTradeNo: tradeNo})
	if err != nil {
		if isTradeNotExist(err) {
			return nil
		}
		return fmt.Errorf("alipay TradeClose: %w", err)
	}
	return nil
}

func isTradeNotExist(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), alipayErrTradeNotExist)
}

func parseAlipayAmount(values ...string) (float64, error) {
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		amount, err := strconv.ParseFloat(raw, 64)
		if err == nil {
			return amount, nil
		}
	}
	return 0, fmt.Errorf("no valid amount field")
}

// Ensure interface compliance.
var (
	_ payment.Provider                 = (*Alipay)(nil)
	_ payment.CancelableProvider       = (*Alipay)(nil)
	_ payment.MerchantIdentityProvider = (*Alipay)(nil)
)
