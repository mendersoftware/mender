// Copyright 2020 Northern.tech AS
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//        http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.
package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/conf"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
	"golang.org/x/crypto/ssh/terminal"
)

type setupOptionsType struct {
	configPath         string
	deviceType         string
	username           string
	password           string
	serverURL          string
	serverIP           string
	serverCert         string
	tenantToken        string
	invPollInterval    int
	retryPollInterval  int
	updatePollInterval int
	hostedMender       bool
	demo               bool
}

// ------------------------------ Setup constants ------------------------------
const ( // state enum
	stateDeviceType = iota
	stateHostedMender
	stateDemoMode
	stateServerURL
	stateServerIP
	stateServerCert
	stateCredentials
	statePolling
	stateDone
	stateInvalid = -1
)

const (
	// Constraint constants
	minimumPollInterval          = 5
	validDeviceRegularExpression = "^[A-Za-z0-9-_]+$"
	validURLRegularExpression    = `(http|https):\/\/(\w+:{0,1}\w*@)?` +
		`(\S+)(:[0-9]+)?((\/\S+?\/)*)(\/|\/([\w#!:.?+=&%@!\-\/]))?`
	validIPRegularExpression = `^([0-9]{1,3}\.){3}[0-9]{1,3}(:[0-9]{1,5})?$`
	// RFC5322 email regex
	validEmailRegularExpression = `(?:[a-z0-9!#$%&'*+/=?^_` + "`" +
		`{|}~-]+(?:\.[a-z0-9!#$%&'*+/=?^_` + "`" +
		`{|}~-]+)*|"(?:[\x01-\x08\x0b\x0c\x0e-\x1f\x21\x23-\x5b\x5d-` +
		`\x7f]|\\[\x01-\x09\x0b\x0c\x0e-\x7f])*")@(?:(?:[a-z0-9]` +
		`(?:[a-z0-9-]*[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]*[a-z0-9])?|` +
		`\[(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]` +
		`|2[0-4][0-9]|[01]?[0-9][0-9]?|[a-z0-9-]*[a-z0-9]:` +
		`(?:[\x01-\x08\x0b\x0c\x0e-\x1f\x21-\x5a\x53-\x7f]|` +
		`\\[\x01-\x09\x0b\x0c\x0e-\x7f])+)\])`

	// Default constants
	defaultServerIP       = "127.0.0.1"
	defaultServerURL      = "https://docker.mender.io"
	defaultInventoryPoll  = 28800 // NOTE: If changing these integer default
	defaultRetryPoll      = 300   //       values, make sure to update the
	defaultUpdatePoll     = 1800  //       corresponding prompt texts below.
	demoInventoryPoll     = 5
	demoRetryPoll         = 30
	demoUpdatePoll        = 5
	demoServerCertificate = "/usr/share/doc/mender-client/examples/demo.crt"
	hostedMenderURL       = "https://hosted.mender.io"

	// Prompt constants
	promptWizard = "Mender Client Setup\n" +
		"===================\n\n" +
		"Setting up the Mender client: The client will " +
		"regularly poll the server to check for updates and report " +
		"its inventory data.\nGet started by first configuring the " +
		"device type and settings for communicating with the server.\n"
	promptDone       = "Mender setup successfully."
	promptDeviceType = "\nThe device type property is used to determine " +
		"which Mender Artifact are compatible with this device.\n" +
		"Enter a name for the device type (e.g. " +
		"raspberrypi3): [%s] "
	promptHostedMender = "\nAre you connecting this device to " +
		"hosted.mender.io? [Y/n] "
	promptCredentials = "Enter your credentials for hosted.mender.io"
	promptDemoMode    = "\nDemo mode uses short poll intervals and assumes the " +
		"default demo server setup. (Recommended for testing.)\n" +
		"Do you want to run the client in demo mode? [Y/n] "
	promptServerIP = "\nSet the IP of the Mender Server: [" +
		defaultServerIP + "] "
	promptServerURL = "\nSet the URL of the Mender Server: [" +
		defaultServerURL + "] "
	promptServerCert = "\nSet the location of the certificate of the " +
		"server; leave blank if using http (not recommended) or a " +
		"certificate from a known authority " +
		"(filepath, for example /etc/mender/server.crt): "
	promptUpdatePoll = "\nSet the update poll interval - the frequency with " +
		"which the client will send an update check request to the " +
		"server, in seconds: [1800]" // (defaultUpdatePoll)
	promptRetryPoll = "\nSet the retry poll interval - the frequency with " +
		"which the client tries to communicate with the server (note: " +
		"the client may attempt more often initially based on the " +
		"previous intervals, but will fall back to this value if the" +
		"server is busy) [300]" // (defaultRetryPoll)
	promptInventoryPoll = "Set the inventory poll interval - the " +
		"frequency with which the client will send inventory data to " +
		"the server, in seconds: [28800]" // (defaultInventoryPoll)
	// Response on invalid input
	rspInvalidDevice = "The device type \"%s\" contains spaces or special " +
		"characters.\nPlease try again: [%s]"
	rspSelectYN     = "Please select Y or N: "
	rspInvalidEmail = "\n\"%s\" does not appear to be a " + // NOTE: format
		"valid email address.\nPlease enter a valid email address: "
	rspHMLogin = "We couldn’t find a Hosted Mender account with those " +
		"credentials.\nPlease try again: "
	rspConnectionError = "There was a problem connecting to " +
		hostedMenderURL + ". \nPlease check your device’s " +
		"connection and try again."
	rspNotSeconds = "The value you entered wasn’t an integer number.\n" +
		"Please enter a number (in seconds): "
	rspInvalidInterval = "Polling interval too short.\nPlease enter a " +
		"value of minimum 5 seconds: " // (minimumPollInterval)
	rspInvalidURL = "Please enter a valid url for the server: "
	rspInvalidIP  = "Please enter a valid IP address: "
	// NOTE: format
	rspFileNotExist = "The file '%s' does not exist.\nPlease try again: "
)

// ---------------------------- END Setup constants ----------------------------

func getDefaultDeviceType() string {
	hostName, err := ioutil.ReadFile("/etc/hostname")
	if err != nil {
		return "unknown"
	}
	devType := string(hostName)
	devType = strings.Trim(devType, "\n")
	return devType
}

type stdinReader struct {
	reader *bufio.Reader
}

func (stdin *stdinReader) promptUser(prompt string, disableEcho bool) (string, error) {
	var rsp string
	var err error
	fmt.Print(prompt)
	if disableEcho {
		pwd, err := terminal.ReadPassword(int(os.Stdin.Fd()))
		if err == nil {
			rsp = string(pwd)
		}
	} else {
		rsp, err = stdin.reader.ReadString('\n')
		if err == nil {
			rsp = rsp[:len(rsp)-1] // trim newline character
		}
	}
	if err != nil {
		return rsp, errors.Wrap(err, "Error reading from stdin.")
	}
	return rsp, err
}

// Prompts the user for yes/no prompt returning true if user entered Y/y
// and false for N/n
func (stdin *stdinReader) promptYN(prompt string,
	defaultYes bool) (bool, error) {
	ret := defaultYes
	rsp, err := stdin.promptUser(prompt, false)
	if err != nil {
		return ret, err
	}
	for {
		if rsp == "Y" || rsp == "y" {
			ret = true
			break
		} else if rsp == "N" || rsp == "n" {
			ret = false
			break
		} else if rsp == "" {
			// default
			break
		}
		rsp, err = stdin.promptUser(rspSelectYN, false)
		if err != nil {
			return ret, err
		}
	}
	return ret, nil
}

// CLI functions for handling implicitly set flags.
func (opts *setupOptionsType) handleImplicitFlags(ctx *cli.Context) error {
	if ctx.IsSet("update-poll") {
		ctx.Set("demo", "false")
		opts.demo = false
		opts.updatePollInterval = ctx.Int("update-poll")
	}
	if ctx.IsSet("inventory-poll") {
		ctx.Set("demo", "false")
		opts.demo = false
		opts.invPollInterval = ctx.Int("inventory-poll")
	}
	if ctx.IsSet("retry-poll") {
		ctx.Set("demo", "false")
		opts.demo = false
		opts.retryPollInterval = ctx.Int("retry-poll")
	}

	if ctx.IsSet("server-url") || ctx.IsSet("server-ip") {
		if ctx.IsSet("server-url") && ctx.IsSet("server-ip") {
			return errors.Errorf(errMsgConflictingArgumentsF,
				"server-url", "server-ip")
		} else if ctx.IsSet("server-ip") {
			ctx.Set("demo", "true")
			opts.demo = true
		}
		ctx.Set("hosted-mender", "false")
		opts.hostedMender = false
	}
	return nil
}

func (opts *setupOptionsType) askCredentials(stdin *stdinReader,
	validEmailRegex *regexp.Regexp) error {
	var err error

	opts.username, err = stdin.promptUser("Email: ", false)
	if err != nil {
		return err
	}
	for {
		if !validEmailRegex.Match(
			[]byte(opts.username)) {

			rsp := fmt.Sprintf(
				rspInvalidEmail,
				opts.username)
			opts.username, err = stdin.promptUser(
				rsp, false)
			if err != nil {
				return err
			}
		} else {
			break
		}
	}
	// Disable stty echo when typing password
	opts.password, err = stdin.promptUser(
		"Password: ", true)
	fmt.Println()
	if err != nil {
		return err
	}
	for {
		if opts.password == "" {
			fmt.Print("Password cannot be " +
				"blank.\nTry again: ")
			opts.password, err = stdin.promptUser(
				"Password: ", true)
			if err != nil {
				return err
			}
		} else {
			break
		}
	}
	return nil
}

func (opts *setupOptionsType) askDeviceType(ctx *cli.Context,
	stdin *stdinReader) (int, error) {
	defaultDevType := getDefaultDeviceType()
	devTypePrompt := fmt.Sprintf(promptDeviceType, defaultDevType)
	validDeviceRegex, err := regexp.Compile(validDeviceRegularExpression)
	if err != nil {
		return stateInvalid, errors.Wrap(err, "Unable to compile regex")
	}
	if validDeviceRegex.Match([]byte(ctx.String("device-type"))) {
		return stateHostedMender, nil
	}
	opts.deviceType, err = stdin.promptUser(devTypePrompt, false)
	if err != nil {
		return stateInvalid, err
	}
	for {
		if opts.deviceType == "" {
			opts.deviceType = defaultDevType
		} else if !validDeviceRegex.Match([]byte(
			opts.deviceType)) {
			rsp := fmt.Sprintf(rspInvalidDevice, opts.deviceType,
				defaultDevType)
			opts.deviceType, err = stdin.promptUser(rsp, false)
		} else {
			break
		}
		if err != nil {
			return stateInvalid, err
		}
	}
	return stateHostedMender, nil
}

func (opts *setupOptionsType) askHostedMender(ctx *cli.Context,
	stdin *stdinReader) (int, error) {
	var state int

	if !ctx.IsSet("hosted-mender") {
		hostedMender, err := stdin.promptYN(
			promptHostedMender, true)
		if err != nil {
			return stateInvalid, err
		}
		opts.hostedMender = hostedMender
	}
	if opts.hostedMender {
		opts.serverURL = hostedMenderURL
		state = stateCredentials
	} else {
		state = stateDemoMode
	}
	return state, nil
}

func (opts *setupOptionsType) askDemoMode(ctx *cli.Context,
	stdin *stdinReader) (int, error) {
	var state int

	if !ctx.IsSet("demo") {
		demo, err := stdin.promptYN(promptDemoMode, true)
		if err != nil {
			return stateInvalid, err
		}
		opts.demo = demo
	}
	if opts.hostedMender {
		if opts.demo {
			state = stateDone
		} else {
			state = statePolling
		}
	} else {
		if opts.demo {
			state = stateServerIP
		} else {
			state = stateServerURL
		}
	}
	return state, nil
}

func (opts *setupOptionsType) askServerURL(ctx *cli.Context,
	stdin *stdinReader) (int, error) {
	validURLRegex, err := regexp.Compile(validURLRegularExpression)
	if err != nil {
		return stateInvalid, errors.Wrap(err, "Unable to compile regex")
	}

	if ctx.IsSet("server-url") {
		opts.serverURL = ctx.String("server-url")
	} else {
		opts.serverURL, err = stdin.promptUser(
			promptServerURL, false)
		if err != nil {
			return stateInvalid, err
		}
	}
	for {
		if opts.serverURL == "" {
			opts.serverURL = defaultServerURL
		} else if !validURLRegex.Match([]byte(opts.serverURL)) {
			opts.serverURL, err = stdin.promptUser(
				rspInvalidURL, false)
			if err != nil {
				return stateInvalid, err
			}
		} else {
			break
		}
	}
	return stateServerCert, nil
}

func (opts *setupOptionsType) askServerIP(ctx *cli.Context,
	stdin *stdinReader) (int, error) {
	validIPRegex, err := regexp.Compile(validIPRegularExpression)
	if err != nil {
		return stateInvalid, errors.Wrap(err, "Unable to compile regex")
	}

	if !ctx.IsSet("server-url") {
		// Set default server URL
		// -- can be modified by flag.
		opts.serverURL = defaultServerURL
	}
	if validIPRegex.Match([]byte(opts.serverIP)) {
		// IP added by cmdline
		return stateDone, nil
	}
	opts.serverIP, err = stdin.promptUser(
		promptServerIP, false)
	if err != nil {
		return stateInvalid, err
	}
	for {
		if opts.serverIP == "" {
			// default
			opts.serverIP = defaultServerIP
			break
		} else if !validIPRegex.Match([]byte(opts.serverIP)) {
			opts.serverIP, err = stdin.promptUser(
				rspInvalidIP, false)
			if err != nil {
				return stateInvalid, err
			}
		} else {
			break
		}
	}
	return stateDone, nil
}

func (opts *setupOptionsType) askServerCert(ctx *cli.Context,
	stdin *stdinReader) (int, error) {
	var err error
	if ctx.IsSet("server-cert") {
		return statePolling, nil
	}
	opts.serverCert, err = stdin.promptUser(
		promptServerCert, false)
	if err != nil {
		return stateInvalid, err
	}
	for {
		if opts.serverCert == "" {
			// No certificates is allowed
			break
		} else if _, err = os.Stat(opts.serverCert); err != nil {
			rsp := fmt.Sprintf(rspFileNotExist, opts.serverCert)
			opts.serverCert, err = stdin.promptUser(
				rsp, false)
			if err != nil {
				return stateInvalid, err
			}
		} else {
			break
		}
	}
	return statePolling, nil
}

func (opts *setupOptionsType) getTenantToken(
	client *http.Client, userToken []byte) error {
	tokReq, err := http.NewRequest(
		"GET",
		hostedMenderURL+
			"/api/management/v1/tenantadm/user/tenant",
		nil)
	if err != nil {
		return errors.Wrap(err,
			"Error creating tenant token request")
	}
	tokReq.Header = map[string][]string{
		"Authorization": {"Bearer " + string(userToken)},
	}
	rsp, err := client.Do(tokReq)
	if rsp != nil {
		defer rsp.Body.Close()
	}
	if err != nil {
		return errors.Wrap(err,
			"Tenant token request FAILED.")
	}
	data, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		return errors.Wrap(err,
			"Reading tenant token FAILED.")
	}
	dataJson := make(map[string]string)
	err = json.Unmarshal(data, &dataJson)
	if err != nil {
		return errors.Wrap(err,
			"Error parsing JSON response.")
	}
	opts.tenantToken = dataJson["tenant_token"]
	log.Info("Successfully requested tenant token.")

	return nil
}

func (opts *setupOptionsType) tryLoginhostedMender(
	stdin *stdinReader, validEmailRegex *regexp.Regexp) error {
	// Test Hosted Mender credentials
	var err error
	var client *http.Client
	var authReq *http.Request
	var response *http.Response
	for {
		client = &http.Client{}
		authReq, err = http.NewRequest(
			"POST",
			hostedMenderURL+
				"/api/management/v1/useradm/auth/login",
			nil)
		if err != nil {
			return errors.Wrap(err, "Error creating "+
				"authorization request.")
		}
		authReq.SetBasicAuth(opts.username, opts.password)
		response, err = client.Do(authReq)

		if response != nil {
			defer response.Body.Close()
		}
		if err != nil {
			// The connection/dns-lookup error is not exported from
			// the "net" package, so use a 'best effort' solution
			// to catch the error by string matching.
			if strings.Contains(err.Error(),
				"Temporary failure in name resolution") {
				fmt.Println(rspConnectionError)
				if err = opts.askCredentials(stdin,
					validEmailRegex); err != nil {
					return err
				}
				continue
			}
			return err
		} else if response.StatusCode == 401 {
			fmt.Println(rspHMLogin)
			err = opts.askCredentials(stdin, validEmailRegex)
			if err != nil {
				return err
			}
		} else if response.StatusCode == 200 {
			break
		} else {
			return errors.Errorf(
				"Unexpected statuscode %d from authentication "+
					"request", response.StatusCode)
		}
	}

	// Get tenant token
	userToken, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return errors.Wrap(err,
			"Error reading authorization token")
	}

	return opts.getTenantToken(client, userToken)
}

func (opts *setupOptionsType) askHostedMenderCredentials(ctx *cli.Context,
	stdin *stdinReader) (int, error) {
	validEmailRegex, err := regexp.Compile(validEmailRegularExpression)
	if err != nil {
		return stateInvalid, errors.Wrap(err, "Unable to compile regex")
	}

	if ctx.IsSet("tenant-token") {
		return stateDemoMode, nil
	}
	if !(ctx.IsSet("username") && ctx.IsSet("password")) {
		fmt.Println(promptCredentials)
		if err := opts.askCredentials(stdin, validEmailRegex); err != nil {
			return stateInvalid, err
		}
	} else if !validEmailRegex.Match([]byte(opts.username)) {
		fmt.Printf(rspInvalidEmail, opts.username)
		if err := opts.askCredentials(stdin, validEmailRegex); err != nil {
			return stateInvalid, err
		}
	}

	err = opts.tryLoginhostedMender(stdin, validEmailRegex)
	if err != nil {
		return stateInvalid, err
	}

	return stateDemoMode, nil
}

func (opts *setupOptionsType) askUpdatePoll(ctx *cli.Context,
	stdin *stdinReader) error {
	if !ctx.IsSet("update-poll") ||
		opts.updatePollInterval < minimumPollInterval {
		rsp, err := stdin.promptUser(
			promptUpdatePoll, false)
		if err != nil {
			return err
		}
		for {
			if rsp == "" {
				opts.updatePollInterval = defaultUpdatePoll
				break
			} else if opts.updatePollInterval, err = strconv.Atoi(
				rsp); err != nil {
				rsp, err = stdin.promptUser(
					rspNotSeconds, false)
			} else if opts.updatePollInterval < minimumPollInterval {
				rsp, err = stdin.promptUser(
					rspInvalidInterval, false)
			} else {
				break
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (opts *setupOptionsType) askInventoryPoll(ctx *cli.Context,
	stdin *stdinReader) error {
	if !ctx.IsSet("inventory-poll") ||
		opts.invPollInterval < minimumPollInterval {
		rsp, err := stdin.promptUser(
			promptInventoryPoll, false)
		if err != nil {
			return err
		}
		for {
			if rsp == "" {
				opts.invPollInterval = defaultInventoryPoll
				break
			} else if opts.invPollInterval, err = strconv.Atoi(
				rsp); err != nil {
				rsp, err = stdin.promptUser(
					rspNotSeconds, false)
			} else if opts.invPollInterval < minimumPollInterval {
				rsp, err = stdin.promptUser(
					rspInvalidInterval, false)
			} else {
				break
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (opts *setupOptionsType) askRetryPoll(ctx *cli.Context,
	stdin *stdinReader) error {
	if !ctx.IsSet("retry-poll") ||
		opts.retryPollInterval < minimumPollInterval {
		rsp, err := stdin.promptUser(
			promptRetryPoll, false)
		if err != nil {
			return err
		}
		for {
			if rsp == "" {
				opts.retryPollInterval = defaultRetryPoll
				break
			} else if opts.retryPollInterval, err = strconv.Atoi(
				rsp); err != nil {
				rsp, err = stdin.promptUser(
					rspNotSeconds, false)
			} else if opts.retryPollInterval < minimumPollInterval {
				rsp, err = stdin.promptUser(
					rspInvalidInterval, false)
			} else {
				break
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (opts *setupOptionsType) askPollingIntervals(ctx *cli.Context,
	stdin *stdinReader) (int, error) {
	if err := opts.askUpdatePoll(ctx, stdin); err != nil {
		return stateInvalid, err
	}
	if err := opts.askInventoryPoll(ctx, stdin); err != nil {
		return stateInvalid, err
	}
	if err := opts.askRetryPoll(ctx, stdin); err != nil {
		return stateInvalid, err
	}
	return stateDone, nil
}

func doSetup(ctx *cli.Context, config *conf.MenderConfigFromFile,
	opts *setupOptionsType) error {
	var err error
	state := stateDeviceType
	stdin := &stdinReader{
		reader: bufio.NewReader(os.Stdin),
	}

	// Prompt 'wizard' message
	if !ctx.Bool("quiet") {
		fmt.Println(promptWizard)
	}

	// Prompt the user for config options if not specified by flags
	for state != stateDone {
		switch state {
		case stateDeviceType:
			state, err = opts.askDeviceType(ctx, stdin)

		case stateHostedMender:
			state, err = opts.askHostedMender(ctx, stdin)

		case stateDemoMode:
			state, err = opts.askDemoMode(ctx, stdin)

		case stateServerURL:
			state, err = opts.askServerURL(ctx, stdin)

		case stateServerIP:
			state, err = opts.askServerIP(ctx, stdin)

		case stateServerCert:
			state, err = opts.askServerCert(ctx, stdin)

		case stateCredentials:
			state, err = opts.askHostedMenderCredentials(ctx, stdin)

		case statePolling:
			state, err = opts.askPollingIntervals(ctx, stdin)
		}
		if err != nil {
			return err
		}
	} // END for {state}
	return opts.saveConfigOptions(config)
}

func (opts *setupOptionsType) saveConfigOptions(
	config *conf.MenderConfigFromFile) error {
	if opts.demo {
		if opts.updatePollInterval > minimumPollInterval {
			config.UpdatePollIntervalSeconds = opts.
				updatePollInterval
		} else {
			config.UpdatePollIntervalSeconds = demoUpdatePoll
		}
		if opts.invPollInterval > minimumPollInterval {
			config.InventoryPollIntervalSeconds = opts.
				invPollInterval
		} else {
			config.InventoryPollIntervalSeconds = demoInventoryPoll
		}
		if opts.retryPollInterval > minimumPollInterval {
			config.RetryPollIntervalSeconds = opts.
				retryPollInterval
		} else {
			config.RetryPollIntervalSeconds = demoRetryPoll
		}
	} else {
		config.InventoryPollIntervalSeconds = opts.invPollInterval
		config.UpdatePollIntervalSeconds = opts.updatePollInterval
		config.RetryPollIntervalSeconds = opts.retryPollInterval
	}

	if opts.demo && !opts.hostedMender {
		config.ServerCertificate = demoServerCertificate
	} else {
		config.ServerCertificate = opts.serverCert
	}

	config.TenantToken = opts.tenantToken

	// Make sure devicetypefile and serverURL is set
	if config.DeviceTypeFile == "" {
		// Default devicetype file as defined in device.go
		config.DeviceTypeFile = conf.DefaultDeviceTypeFile
	}
	config.Servers = []client.MenderServer{
		{
			ServerURL: opts.serverURL},
	}
	// Extract schema to set ClientProtocol
	re, err := regexp.Compile(validURLRegularExpression)
	if err != nil {
		return errors.Wrap(err, "Unable to compile regular expression")
	}
	serverURL := opts.serverURL
	schema := re.ReplaceAllString(serverURL, "$1")
	config.ClientProtocol = schema

	// Avoid possibility of conflicting ServerURL from an old config
	config.ServerURL = ""

	if err := conf.SaveConfigFile(config, opts.configPath); err != nil {
		return err
	}
	err = ioutil.WriteFile(config.DeviceTypeFile,
		[]byte("device_type="+opts.deviceType), 0644)
	if err != nil {
		return errors.Wrap(err, "Error writing to devicefile.")
	}
	if opts.demo && !opts.hostedMender {
		opts.maybeAddHostLookup()
	}
	return nil
}

func (opts *setupOptionsType) maybeAddHostLookup() {
	// Regex: $1: schema, $2: URL, $3: path
	re, err := regexp.Compile(`(https?://)?(.*)(/.*)?`)
	if err != nil {
		log.Warn("Unable to compile regular expression for parsing " +
			"server URL.")
		return
	}
	// strip schema and path
	host := re.ReplaceAllString(opts.serverURL, "$2")

	// Add "s3.SERVER_URL" as well. This is only called in demo mode, so it
	// should be a safe assumption.
	route := fmt.Sprintf("%-15s %s s3.%s", opts.serverIP, host, host)

	f, err := os.OpenFile("/etc/hosts", os.O_RDWR, 0644)
	if err != nil {
		log.Warnf("Unable to open \"/etc/hosts\" for appending "+
			"local route \"%s\": %s", route, err.Error())
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)

	// Check if route already exists
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), host) {
			return
		}
	}

	// Seek to last character
	_, err = f.Seek(-1, os.SEEK_END)
	if err != nil {
		log.Warnf("Unable to add route \"%s\" to \"/etc/hosts\": %s",
			route, err.Error())
	}
	routeLine := "\n" + route + "\n"
	// Remove newline from routeLine string if there already is one
	lastChar := make([]byte, 1)
	if _, err := f.Read(lastChar); err == nil &&
		lastChar[0] == byte('\n') {
		routeLine = routeLine[1:]
	}

	_, err = f.WriteString(routeLine)
	if err != nil {
		log.Warnf("Unable to add route \"%s\" to \"/etc/hosts\": %s",
			route, err.Error())
	}
}
