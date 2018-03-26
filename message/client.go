package message

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/northbright/uuid"
)

// Client is used to make HTTP requests of aliyun API message serviices.
// A client should be resused to send SMS, make single TTS call...
type Client struct {
	// Use http.Client.Do().
	http.Client
	// accessKeyID is the access key ID generated by user.
	accessKeyID string
	// accessKeySecret is the access key secret generated by user.
	accessKeySecret string
}

// Response is the common response for aliyun message services APIs.
type Response struct {
	// RequestID is the request ID. e.g. "8906582E-6722".
	RequestID string `json:"RequestId"` // Code is the status code. e.g. "OK", "SignatureDoesNotMatch".
	Code      string `json:"Code"`      // Message is the detail message for the status code. e.g. "OK", Specified signature is not matched with our calculation...".
	Message   string `json:"Message"`
	// BizID is the business ID. It can be used to query the status of SMS. e.g. "134523^4351232".
}

// SMSResponse is the response of HTTP request of sending SMS.
type SMSResponse struct {
	Response
	BizID string `json:"BizId"`
}

// SingleCallByTTSResponse is the response of HTTP request of make single call by TTS.
type SingleCallByTTSResponse struct {
	Response
	CallID string `json:"CallId"`
}

// NewClient creates a new client.
//
// It accepts 2 parameters: access key ID and secret.
// Both of them are generated by user in aliyun control panel.
func NewClient(accessKeyID, accessKeySecret string) *Client {
	return &Client{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
	}
}

// SpecialURLEncode follows aliyun's POP protocol to do special URL encoding.
func SpecialURLEncode(str string) string {
	encodedStr := url.QueryEscape(str)
	encodedStr = strings.Replace(encodedStr, "+", "%20", -1)
	encodedStr = strings.Replace(encodedStr, "*", "%2A", -1)
	encodedStr = strings.Replace(encodedStr, "%7E", "~", -1)
	return encodedStr
}

// SetDefaultCommonParams sets the default common parameters for aliyun services.
func (c *Client) SetDefaultCommonParams(v url.Values) {
	// Set access key ID.
	v.Set("AccessKeyId", c.accessKeyID)

	// Set default common parameters
	v.Set("Timestamp", GenTimestamp(time.Now()))
	v.Set("Format", "JSON")
	v.Set("SignatureMethod", "HMAC-SHA1")
	v.Set("SignatureVersion", "1.0")
	UUID, _ := uuid.New()
	v.Set("SignatureNonce", UUID)
}

// SignedString follow aliyun's POP protocol to generate the signature.
// httpMethod: follow aliyun doc. e.g. "GET" for sending SMS and single TTS call.
func (c *Client) SignedString(httpMethod, sortedQueryStr string) string {
	str := httpMethod + "&" + url.QueryEscape("/") + "&" + SpecialURLEncode(sortedQueryStr)

	// HMAC-SHA1
	// aliyun requires appending "&" after access key secret.
	mac := hmac.New(sha1.New, []byte(c.accessKeySecret+"&"))
	mac.Write([]byte(str))

	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return SpecialURLEncode(sign)
}

// SendSMS sends the SMS to phone numbers.
//
// phoneNumbers: one or more phone numbers. aliyun recommends to send SMS to only one phone number once for validation code.
// signName: permitted signature name. You may apply one ore more signature names in aliyun's control panel.
// templateCode: permitted template code. You may apply one or more template code in aliyun's control panel.
// templateParam: JSON to render the template. e.g. {"code":"1234","product":"ytx"}.
// params: optional parameters for sending SMS. In most case, no need to pass params.
// You may also specify params by helper functions. e.g. Timestamp(), SignatureNonce().
//
// It returns success status, response and error.
//
// For example:
//
// c := message.NewClient(accessKeyID, accessKeySecret)
//
// ok, resp, err := c.SendSMS([]string{"13800138000"}, "my_product", "SMS_0000", `{"code":"1234","product":"ytx"}`)
func (c *Client) SendSMS(phoneNumbers []string, signName, templateCode, templateParam string, params ...Param) (bool, *SMSResponse, error) {
	v := url.Values{}
	// Set default common parameters for aliyun services.
	c.SetDefaultCommonParams(v)

	// Set default business parameters for sending SMS.
	v.Set("Action", "SendSms")
	v.Set("Version", "2017-05-25")
	v.Set("RegionId", "cn-hangzhou")

	// Set required business parameters
	v.Set("PhoneNumbers", GenPhoneNumbersStr(phoneNumbers))
	v.Set("SignName", signName)
	v.Set("TemplateCode", templateCode)
	v.Set("TemplateParam", templateParam)

	// Override parameters if need.
	for _, param := range params {
		param.f(v)
	}

	// Get sorted query string by keys.
	sortedQueryStr := v.Encode()

	// Get signature.
	sign := c.SignedString("GET", sortedQueryStr)

	// Make final query string with signature.
	rawQuery := fmt.Sprintf("Signature=%s&%s", sign, sortedQueryStr)

	// New a URL with host, raw query.
	u := &url.URL{
		Scheme:   "http",
		Host:     "dysmsapi.aliyuncs.com",
		Path:     "/",
		RawQuery: rawQuery,
	}

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return false, nil, err
	}

	resp, err := c.Do(req)
	if err != nil {
		return false, nil, err
	}
	defer resp.Body.Close()

	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, nil, err
	}

	// Parse JSON response
	response := &SMSResponse{}
	if err = json.Unmarshal(buf, response); err != nil {
		return false, nil, err
	}

	if strings.ToUpper(response.Code) != "OK" {
		return false, response, nil
	}
	return true, response, nil

}

// MakeSingleCallByTTS makes the single call by TTS.
//
// calledShowNumber: called show number to users. It can be purchased at aliyun's control panel.
// calledNumber: phone number to make single call.
// ttsCode: permitted TTS template code. You may apply one or more template code in aliyun's control panel.
// ttsParam: JSON to render the template. e.g. {"code":"1234","product":"ytx"}.
// params: optional parameters for sending SMS. In most case, no need to pass params.
// You may also specify params by helper functions. e.g. Timestamp(), SignatureNonce().
//
// It returns success status, response and error.
//
// For example:
//
// c := message.NewClient(accessKeyID, accessKeySecret)
//
// ok, resp, err := c.MakeSingleCallByTTS("02560000000", "1500000000", "TTS_0000", `{"code":"1234","product":"ytx"}`)
func (c *Client) MakeSingleCallByTTS(calledShowNumber, calledNumber, ttsCode, ttsParam string, params ...Param) (bool, *SingleCallByTTSResponse, error) {
	v := url.Values{}
	// Set default common parameters for aliyun services.
	c.SetDefaultCommonParams(v)

	// Set default business parameters for sending SMS.
	v.Set("Action", "SingleCallByTts")
	v.Set("Version", "2017-05-25")
	v.Set("RegionId", "cn-hangzhou")

	// Set required business parameters
	v.Set("CalledShowNumber", calledShowNumber)
	v.Set("CalledNumber", calledNumber)
	v.Set("TtsCode", ttsCode)
	v.Set("TtsParam", ttsParam)

	// Override parameters if need.
	for _, param := range params {
		param.f(v)
	}

	// Get sorted query string by keys.
	sortedQueryStr := v.Encode()

	// Get signature.
	sign := c.SignedString("GET", sortedQueryStr)

	// Make final query string with signature.
	rawQuery := fmt.Sprintf("Signature=%s&%s", sign, sortedQueryStr)

	// New a URL with host, raw query.
	u := &url.URL{
		Scheme:   "http",
		Host:     "dyvmsapi.aliyuncs.com",
		Path:     "/",
		RawQuery: rawQuery,
	}

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return false, nil, err
	}

	resp, err := c.Do(req)
	if err != nil {
		return false, nil, err
	}
	defer resp.Body.Close()

	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, nil, err
	}

	// Parse JSON response
	response := &SingleCallByTTSResponse{}
	if err = json.Unmarshal(buf, response); err != nil {
		return false, nil, err
	}

	if strings.ToUpper(response.Code) != "OK" {
		return false, response, nil
	}
	return true, response, nil
}
