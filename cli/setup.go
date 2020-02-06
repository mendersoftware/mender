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

	"github.com/alfrunes/cli"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh/terminal"
)

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
		"raspberrypi3-raspbian): [%s] "
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
func handleImplicitFlags(ctx *cli.Context) error {
	if _, isSet := ctx.Int("update-poll"); isSet {
		ctx.Set("demo", "false")
	} else if _, isSet := ctx.Int("inventory-poll"); isSet {
		ctx.Set("demo", "false")
	} else if _, isSet := ctx.Int("retry-poll"); isSet {
		ctx.Set("demo", "false")
	}

	_, gotURL := ctx.String("server-url")
	_, gotIP := ctx.String("server-ip")
	if gotIP && gotURL {
		return errors.Errorf(errMsgConflictingArgumentsF,
			"server-url", "server-ip")
	} else if gotIP || gotURL {
		ctx.Set("hosted-mender", "false")

		if gotIP {
			ctx.Set("demo", "true")
		}
	}

	return nil
}

func (stdin *stdinReader) askCredentials(
	validEmailRegex *regexp.Regexp,
) (string, string, error) {
	username, err := stdin.promptUser("Email: ", false)
	if err != nil {
		return "", "", err
	}
	for {
		if !validEmailRegex.Match(
			[]byte(username)) {

			rsp := fmt.Sprintf(
				rspInvalidEmail,
				username)
			username, err = stdin.promptUser(
				rsp, false)
			if err != nil {
				return "", "", err
			}
		} else {
			break
		}
	}
	// Disable stty echo when typing password
	password, err := stdin.promptUser(
		"Password: ", true)
	fmt.Println()
	if err != nil {
		return "", "", err
	}
	for {
		if password == "" {
			fmt.Print("Password cannot be " +
				"blank.\nTry again: ")
			password, err = stdin.promptUser(
				"Password: ", true)
			if err != nil {
				return "", "", err
			}
		} else {
			break
		}
	}
	return username, password, nil
}

func askDeviceType(ctx *cli.Context,
	stdin *stdinReader) (int, error) {
	defaultDevType := getDefaultDeviceType()
	deviceType, isSet := ctx.String("device-type")
	devTypePrompt := fmt.Sprintf(promptDeviceType, defaultDevType)
	validDeviceRegex, err := regexp.Compile(validDeviceRegularExpression)
	if err != nil {
		return stateInvalid, errors.Wrap(err, "Unable to compile regex")
	}
	if isSet && validDeviceRegex.Match([]byte(deviceType)) {
		return stateHostedMender, nil
	}
	deviceType, err = stdin.promptUser(devTypePrompt, false)
	if err != nil {
		return stateInvalid, err
	}
	for {
		if deviceType == "" {
			deviceType = defaultDevType
		} else if !validDeviceRegex.Match([]byte(
			deviceType)) {
			rsp := fmt.Sprintf(rspInvalidDevice, deviceType,
				defaultDevType)
			deviceType, err = stdin.promptUser(rsp, false)
		} else {
			break
		}
		if err != nil {
			return stateInvalid, err
		}
	}
	ctx.Set("device-type", deviceType)
	return stateHostedMender, nil
}

func askHostedMender(ctx *cli.Context,
	stdin *stdinReader) (int, error) {
	var state int
	var err error
	hostedMender, isSet := ctx.Bool("hosted-mender")

	if !isSet {
		hostedMender, err = stdin.promptYN(
			promptHostedMender, true)
		if err != nil {
			return stateInvalid, err
		}
		hostedMender = hostedMender
	}
	if hostedMender {
		state = stateCredentials
		ctx.Set("server-url", hostedMenderURL)
		ctx.Set("hosted-mender", "true")
	} else {
		state = stateDemoMode
		ctx.Set("hosted-mender", "false")
	}
	return state, nil
}

func askDemoMode(ctx *cli.Context, stdin *stdinReader) (int, error) {
	var state int
	var err error
	demo, isSet := ctx.Bool("demo")
	hostedMender, _ := ctx.Bool("hosted-mender")

	if !isSet {
		demo, err = stdin.promptYN(promptDemoMode, true)
		if err != nil {
			return stateInvalid, err
		}
	}
	if demo {
		ctx.Set("demo", "true")
		if hostedMender {
			state = stateDone
		} else {
			state = stateServerIP
		}
	} else {
		ctx.Set("demo", "false")
		if hostedMender {
			state = statePolling
		} else {
			state = stateServerURL
		}
	}

	return state, nil
}

func askServerURL(ctx *cli.Context,
	stdin *stdinReader) (int, error) {
	validURLRegex, err := regexp.Compile(validURLRegularExpression)
	if err != nil {
		return stateInvalid, errors.Wrap(err, "Unable to compile regex")
	}

	serverURL, isSet := ctx.String("server-url")

	if !isSet {
		serverURL, err = stdin.promptUser(
			promptServerURL, false)
		if err != nil {
			return stateInvalid, err
		}
	}
	for {
		if serverURL == "" {
			serverURL = defaultServerURL
		} else if !validURLRegex.Match([]byte(serverURL)) {
			serverURL, err = stdin.promptUser(
				rspInvalidURL, false)
			if err != nil {
				return stateInvalid, err
			}
		} else {
			break
		}
	}
	ctx.Set("server-url", serverURL)
	return stateServerCert, nil
}

func askServerIP(ctx *cli.Context, stdin *stdinReader) (int, error) {
	validIPRegex, err := regexp.Compile(validIPRegularExpression)
	if err != nil {
		return stateInvalid, errors.Wrap(err, "Unable to compile regex")
	}

	serverIP, isSet := ctx.String("server-ip")

	if isSet && validIPRegex.Match([]byte(serverIP)) {
		// IP added by cmdline
		return stateDone, nil
	}
	serverIP, err = stdin.promptUser(promptServerIP, false)
	if err != nil {
		return stateInvalid, err
	}
	for {
		if serverIP == "" {
			// default
			serverIP = defaultServerIP
			break
		} else if !validIPRegex.Match([]byte(serverIP)) {
			serverIP, err = stdin.promptUser(rspInvalidIP, false)
			if err != nil {
				return stateInvalid, err
			}
		} else {
			break
		}
	}
	ctx.Set("server-ip", serverIP)
	return stateDone, nil
}

func askServerCert(ctx *cli.Context, stdin *stdinReader) (int, error) {
	var err error
	serverCert, isSet := ctx.String("trusted-certs")
	if isSet {
		return statePolling, nil
	}
	serverCert, err = stdin.promptUser(
		promptServerCert, false)
	if err != nil {
		return stateInvalid, err
	}
	for {
		if serverCert == "" {
			// No certificates is allowed
			break
		} else if _, err = os.Stat(serverCert); err != nil {
			rsp := fmt.Sprintf(rspFileNotExist, serverCert)
			serverCert, err = stdin.promptUser(
				rsp, false)
			if err != nil {
				return stateInvalid, err
			}
		} else {
			break
		}
	}
	ctx.Set("trusted-certs", serverCert)
	return statePolling, nil
}

func getTenantToken(
	ctx *cli.Context,
	client *http.Client,
	userToken []byte,
) error {
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
	ctx.Set("tenant-token", dataJson["tenant_token"])
	log.Info("Successfully received tenant token.")

	return nil
}

func tryLoginhostedMender(ctx *cli.Context, stdin *stdinReader, validEmailRegex *regexp.Regexp) error {
	// Test Hosted Mender credentials
	var err error
	var client *http.Client
	var authReq *http.Request
	var response *http.Response
	username, _ := ctx.String("username")
	password, _ := ctx.String("password")
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
		authReq.SetBasicAuth(username, password)
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
				if username, password, err = stdin.
					askCredentials(
						validEmailRegex); err != nil {
					return err
				}
				continue
			}
			return err
		} else if response.StatusCode == 401 {
			fmt.Println(rspHMLogin)
			username, password, err = stdin.
				askCredentials(validEmailRegex)
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

	return getTenantToken(ctx, client, userToken)
}

func askHostedMenderCredentials(ctx *cli.Context,
	stdin *stdinReader) (int, error) {
	username, userSet := ctx.String("username")
	password, passSet := ctx.String("password")
	_, tokSet := ctx.String("tenant-token")

	validEmailRegex, err := regexp.Compile(validEmailRegularExpression)
	if err != nil {
		return stateInvalid, errors.Wrap(err, "Unable to compile regex")
	}

	if tokSet {
		return stateDemoMode, nil
	} else if !(userSet && passSet) {
		fmt.Println(promptCredentials)
		if username, password, err = stdin.
			askCredentials(validEmailRegex); err != nil {
			return stateInvalid, err
		}
	} else if !validEmailRegex.Match([]byte(username)) {
		fmt.Printf(rspInvalidEmail, username)
		if username, password, err = stdin.
			askCredentials(validEmailRegex); err != nil {
			return stateInvalid, err
		}
	}

	ctx.Set("username", username)
	ctx.Set("password", password)
	err = tryLoginhostedMender(ctx, stdin, validEmailRegex)
	if err != nil {
		return stateInvalid, err
	}

	return stateDemoMode, nil
}

func askUpdatePoll(ctx *cli.Context, stdin *stdinReader) error {
	if updatePoll, isSet := ctx.Int("update-poll"); !isSet ||
		updatePoll < minimumPollInterval {
		rsp, err := stdin.promptUser(
			promptUpdatePoll, false)
		if err != nil {
			return err
		}
		for {
			if rsp == "" {
				rsp = fmt.Sprintf("%d", defaultUpdatePoll)
				break
			} else if updatePoll, err = strconv.Atoi(
				rsp); err != nil {
				rsp, err = stdin.promptUser(
					rspNotSeconds, false)
			} else if updatePoll < minimumPollInterval {
				rsp, err = stdin.promptUser(
					rspInvalidInterval, false)
			} else {
				break
			}
			if err != nil {
				return err
			}
		}
		ctx.Set("update-poll", rsp)
	}
	return nil
}

func askInventoryPoll(ctx *cli.Context, stdin *stdinReader) error {
	if inventoryPoll, isSet := ctx.Int("inventory-poll"); !isSet ||
		inventoryPoll < minimumPollInterval {
		rsp, err := stdin.promptUser(
			promptInventoryPoll, false)
		if err != nil {
			return err
		}
		for {
			if rsp == "" {
				rsp = fmt.Sprintf("%d", defaultInventoryPoll)
				break
			} else if inventoryPoll, err = strconv.Atoi(
				rsp); err != nil {
				rsp, err = stdin.promptUser(
					rspNotSeconds, false)
			} else if inventoryPoll < minimumPollInterval {
				rsp, err = stdin.promptUser(
					rspInvalidInterval, false)
			} else {
				break
			}
			if err != nil {
				return err
			}
		}
		ctx.Set("inventory-poll", rsp)
	}
	return nil
}

func askRetryPoll(ctx *cli.Context, stdin *stdinReader) error {
	if retryPoll, isSet := ctx.Int("retry-poll"); !isSet ||
		retryPoll < minimumPollInterval {
		rsp, err := stdin.promptUser(
			promptRetryPoll, false)
		if err != nil {
			return err
		}
		for {
			if rsp == "" {
				rsp = fmt.Sprintf("%d", defaultRetryPoll)
				break
			} else if retryPoll, err = strconv.Atoi(
				rsp); err != nil {
				rsp, err = stdin.promptUser(
					rspNotSeconds, false)
			} else if retryPoll < minimumPollInterval {
				rsp, err = stdin.promptUser(
					rspInvalidInterval, false)
			} else {
				break
			}
			if err != nil {
				return err
			}
		}
		ctx.Set("retry-poll", rsp)
	}
	return nil
}

func askPollingIntervals(ctx *cli.Context, stdin *stdinReader) (int, error) {
	if err := askUpdatePoll(ctx, stdin); err != nil {
		return stateInvalid, err
	}
	if err := askInventoryPoll(ctx, stdin); err != nil {
		return stateInvalid, err
	}
	if err := askRetryPoll(ctx, stdin); err != nil {
		return stateInvalid, err
	}
	return stateDone, nil
}

func doSetup(ctx *cli.Context, config *conf.MenderConfigFromFile) error {
	var err error
	state := stateDeviceType
	stdin := &stdinReader{
		reader: bufio.NewReader(os.Stdin),
	}

	// Prompt 'wizard' message
	if quiet, _ := ctx.Bool("quiet"); quiet {
		fmt.Println(promptWizard)
	}

	// Prompt the user for config options if not specified by flags
	for state != stateDone {
		switch state {
		case stateDeviceType:
			state, err = askDeviceType(ctx, stdin)

		case stateHostedMender:
			state, err = askHostedMender(ctx, stdin)

		case stateDemoMode:
			state, err = askDemoMode(ctx, stdin)

		case stateServerURL:
			state, err = askServerURL(ctx, stdin)

		case stateServerIP:
			state, err = askServerIP(ctx, stdin)

		case stateServerCert:
			state, err = askServerCert(ctx, stdin)

		case stateCredentials:
			state, err = askHostedMenderCredentials(ctx, stdin)

		case statePolling:
			state, err = askPollingIntervals(ctx, stdin)
		}
		if err != nil {
			return err
		}
	} // END for {state}
	return saveConfigOptions(ctx, config)
}

func saveConfigOptions(ctx *cli.Context, config *conf.MenderConfigFromFile) error {

	demo, _ := ctx.Bool("demo")
	hostedMender, _ := ctx.Bool("hosted-mender")
	updatePoll, _ := ctx.Int("update-poll")
	inventoryPoll, _ := ctx.Int("inventory-poll")
	retryPoll, _ := ctx.Int("retry-poll")
	configPath, _ := ctx.String("config")
	deviceType, _ := ctx.String("device-type")
	serverCert, _ := ctx.String("trusted-certs")
	serverIP, _ := ctx.String("server-ip")
	serverURL, _ := ctx.String("server-url")
	tenantToken, _ := ctx.String("tenant-token")

	if demo {
		if updatePoll > minimumPollInterval {
			config.UpdatePollIntervalSeconds = updatePoll
		} else {
			config.UpdatePollIntervalSeconds = demoUpdatePoll
		}
		if inventoryPoll > minimumPollInterval {
			config.InventoryPollIntervalSeconds = inventoryPoll
		} else {
			config.InventoryPollIntervalSeconds = demoInventoryPoll
		}
		if retryPoll > minimumPollInterval {
			config.RetryPollIntervalSeconds = retryPoll
		} else {
			config.RetryPollIntervalSeconds = demoRetryPoll
		}
	} else {
		config.InventoryPollIntervalSeconds = inventoryPoll
		config.UpdatePollIntervalSeconds = updatePoll
		config.RetryPollIntervalSeconds = retryPoll
	}

	if demo && !hostedMender {
		config.ServerCertificate = demoServerCertificate
	} else {
		config.ServerCertificate = serverCert
	}

	config.TenantToken = tenantToken

	// Make sure devicetypefile and serverURL is set
	if config.DeviceTypeFile == "" {
		// Default devicetype file as defined in device.go
		config.DeviceTypeFile = conf.DefaultDeviceTypeFile
	}
	config.Servers = []client.MenderServer{
		{
			ServerURL: serverURL},
	}
	// Extract schema to set ClientProtocol
	re, err := regexp.Compile(validURLRegularExpression)
	if err != nil {
		return errors.Wrap(err, "Unable to compile regular expression")
	}
	schema := re.ReplaceAllString(serverURL, "$1")
	config.ClientProtocol = schema

	// Avoid possibility of conflicting ServerURL from an old config
	config.ServerURL = ""

	if err := conf.SaveConfigFile(config, configPath); err != nil {
		return err
	}
	err = ioutil.WriteFile(config.DeviceTypeFile,
		[]byte("device_type="+deviceType), 0644)
	if err != nil {
		return errors.Wrap(err, "Error writing to devicefile.")
	}
	if demo && !hostedMender {
		maybeAddHostLookup(serverIP, serverURL)
	}
	return nil
}

func maybeAddHostLookup(serverIP, serverURL string) {
	// Regex: $1: schema, $2: URL, $3: path
	re, err := regexp.Compile(`(https?://)?(.*)(/.*)?`)
	if err != nil {
		log.Warn("Unable to compile regular expression for parsing " +
			"server URL.")
		return
	}
	// strip schema and path
	host := re.ReplaceAllString(serverURL, "$2")

	// Add "s3.SERVER_URL" as well. This is only called in demo mode, so it
	// should be a safe assumption.
	route := fmt.Sprintf("%-15s %s s3.%s", serverIP, host, host)

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
