// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/memory"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/registry"
	"github.com/clictl/cli/internal/updater"
	"github.com/clictl/cli/internal/vault"
)

var loginAPIKey string

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with clictl",
	Long: `Log in to clictl using an API key or web browser OAuth.

  # API Key login (requires pre-generated key)
  clictl login --api-key CLAK-...

  # Browser-based OAuth login (recommended)
  clictl login

Saves credentials to ~/.clictl/config.yaml.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)

		if loginAPIKey != "" {
			return runAPIKeyLogin(cmd, cfg)
		}
		return runBrowserLogin(cmd, cfg)
	},
}

func runAPIKeyLogin(cmd *cobra.Command, cfg *config.Config) error {
	cfg.Auth.APIKey = loginAPIKey
	cfg.Auth.AccessToken = ""
	cfg.Auth.RefreshToken = ""
	cfg.Auth.ExpiresAt = ""

	client := registry.NewClient(cfg.APIURL, nil, true)
	client.AuthToken = loginAPIKey

	user, err := client.GetCurrentUser(cmd.Context())
	if err != nil {
		return fmt.Errorf("invalid API key: %w", err)
	}

	// Store API key in vault for secure resolution
	storeAuthInVault("CLICTL_API_KEY", loginAPIKey)

	// Set active workspace if not already set
	if cfg.Auth.ActiveWorkspace == "" {
		setDefaultWorkspace(cmd.Context(), client, cfg)
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	printLoginSuccess(user, cfg)

	// Sync workspace memories after successful login
	syncWorkspaceMemories(cmd.Context(), cfg)

	// Background version check
	checkCLIVersionAfterLogin(cfg)

	return nil
}


func runBrowserLogin(cmd *cobra.Command, cfg *config.Config) error {
	port, err := findFreePort()
	if err != nil {
		return fmt.Errorf("finding free port: %w", err)
	}

	codeVerifier, codeChallenge := generatePKCE()
	stateToken := generateStateToken()
	oauthURL := buildOAuthURL(cfg.APIURL, port, stateToken, codeChallenge)

	fmt.Println("\033[1m> clictl login\033[0m")
	fmt.Println()
	fmt.Println("  Authenticate by opening this URL in your browser:")
	fmt.Printf("\n  %s\n\n", oauthURL)

	// Try to open browser automatically
	if err := openBrowser(oauthURL); err == nil {
		fmt.Println("  Browser opened.")
	} else {
		fmt.Println("  Could not open browser automatically. Copy the URL above.")
	}

	fmt.Println()
	fmt.Println("  Waiting for callback... or paste your token below and press Enter:")
	fmt.Println()

	// Race: callback server vs manual token input. First one wins.
	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
	defer cancel()

	codeCh := make(chan string, 1)

	// Start callback server in background
	go func() {
		if c, err := startCallbackServer(ctx, port, stateToken); err == nil {
			codeCh <- c
		}
	}()

	// Accept manual token input in background
	go func() {
		var input string
		fmt.Print("  Token: ")
		fmt.Scanln(&input)
		input = strings.TrimSpace(input)
		if input != "" {
			codeCh <- input
		}
	}()

	var code string
	select {
	case code = <-codeCh:
		cancel() // stop the other goroutine
	case <-ctx.Done():
		return fmt.Errorf("authentication timed out (5 minutes)")
	}

	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)
	apiClient := registry.NewClient(cfg.APIURL, nil, true)
	tokenResp, err := apiClient.ExchangeOAuthCode(cmd.Context(), code, codeVerifier, redirectURI)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}

	cfg.Auth.AccessToken = tokenResp.AccessToken
	cfg.Auth.RefreshToken = tokenResp.RefreshToken
	cfg.Auth.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Format(time.RFC3339)

	// Set workspace from OAuth response (user selected it in the browser)
	if tokenResp.WorkspaceSlug != "" {
		cfg.Auth.ActiveWorkspace = tokenResp.WorkspaceSlug
	}
	cfg.Auth.APIKey = ""

	// Store tokens in vault for secure resolution
	storeAuthInVault("CLICTL_ACCESS_TOKEN", tokenResp.AccessToken)
	storeAuthInVault("CLICTL_REFRESH_TOKEN", tokenResp.RefreshToken)

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	// Verify tokens and fetch user
	verifyClient := registry.NewClient(cfg.APIURL, nil, true)
	verifyClient.AuthToken = tokenResp.AccessToken
	user, err := verifyClient.GetCurrentUser(cmd.Context())
	if err != nil {
		return fmt.Errorf("token validation failed: %w", err)
	}

	// Set active workspace if not already set
	if cfg.Auth.ActiveWorkspace == "" {
		setDefaultWorkspace(cmd.Context(), verifyClient, cfg)
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	printLoginSuccess(user, cfg)

	// Sync workspace memories after successful login
	syncWorkspaceMemories(cmd.Context(), cfg)

	// Background version check
	checkCLIVersionAfterLogin(cfg)

	return nil
}

func printLoginSuccess(user *models.UserInfo, cfg *config.Config) {
	name := user.Username
	if user.FullName != "" {
		name = user.FullName
	}

	fmt.Println()
	fmt.Printf("Logged in as %s (%s)\n", name, user.Email)
	if cfg.Auth.ActiveWorkspace != "" {
		fmt.Printf("Workspace: %s\n", cfg.Auth.ActiveWorkspace)
	}
	fmt.Println("Credentials saved to ~/.clictl/config.yaml")
}

// setDefaultWorkspace fetches the user's workspaces and lets the user choose.
func setDefaultWorkspace(ctx context.Context, client *registry.Client, cfg *config.Config) {
	workspaces, err := client.GetWorkspaces(ctx)
	if err != nil || len(workspaces) == 0 {
		return
	}

	if len(workspaces) == 1 {
		cfg.Auth.ActiveWorkspace = workspaces[0].Slug
		return
	}

	// Multiple workspaces - let user choose
	fmt.Println()
	fmt.Println("Select a workspace:")
	for i, ws := range workspaces {
		label := ws.Name
		if ws.Slug != ws.Name {
			label = fmt.Sprintf("%s (%s)", ws.Name, ws.Slug)
		}
		if ws.IsPersonal {
			label += " [personal]"
		}
		fmt.Printf("  %d. %s\n", i+1, label)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("  Enter number (1-%d): ", len(workspaces))
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			// Default to first
			cfg.Auth.ActiveWorkspace = workspaces[0].Slug
			return
		}
		var choice int
		if _, err := fmt.Sscanf(input, "%d", &choice); err == nil && choice >= 1 && choice <= len(workspaces) {
			cfg.Auth.ActiveWorkspace = workspaces[choice-1].Slug
			return
		}
		fmt.Println("  Invalid choice. Try again.")
	}
}

func findFreePort() (int, error) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{Port: 0})
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func generateStateToken() string {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func generatePKCE() (verifier, challenge string) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)

	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return
}

func buildOAuthURL(apiURL string, port int, stateToken, codeChallenge string) string {
	baseURL := fmt.Sprintf("%s/api/v1/oauth/authorize/", apiURL)
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", "clictl")
	params.Set("redirect_uri", fmt.Sprintf("http://127.0.0.1:%d/callback", port))
	params.Set("state", stateToken)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	return baseURL + "?" + params.Encode()
}

func openBrowser(oauthURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", oauthURL)
	case "linux":
		cmd = exec.Command("xdg-open", oauthURL)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", oauthURL)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
	return cmd.Run()
}

type callbackRequest struct {
	Code  string
	State string
	Error string
}

func startCallbackServer(ctx context.Context, port int, expectedState string) (string, error) {
	callbackChan := make(chan callbackRequest, 1)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")
		errMsg := r.URL.Query().Get("error")

		callbackChan <- callbackRequest{
			Code:  code,
			State: state,
			Error: errMsg,
		}

		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `<!DOCTYPE html>
<html>
<head>
<title>clictl - Ready</title>
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: 'SF Mono', 'Fira Code', 'JetBrains Mono', monospace;
    background: #0a0a0a;
    color: #e5e5e5;
    height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: hidden;
  }
  .container {
    text-align: center;
    max-width: 520px;
    padding: 0 24px;
    animation: fadeIn 0.6s ease-out;
  }
  @keyframes fadeIn {
    from { opacity: 0; transform: translateY(16px); }
    to { opacity: 1; transform: translateY(0); }
  }
  .prompt {
    font-size: 14px;
    color: #737373;
    margin-bottom: 32px;
  }
  .prompt span { color: #22c55e; }
  .check {
    width: 64px;
    height: 64px;
    margin: 0 auto 24px;
    border-radius: 50%;
    background: #14532d;
    display: flex;
    align-items: center;
    justify-content: center;
    animation: scaleIn 0.4s ease-out 0.2s both;
  }
  @keyframes scaleIn {
    from { transform: scale(0); }
    to { transform: scale(1); }
  }
  .check svg { width: 32px; height: 32px; color: #22c55e; }
  h1 {
    font-size: 28px;
    font-weight: 700;
    color: #ffffff;
    margin-bottom: 12px;
    letter-spacing: -0.5px;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
  }
  .subtitle {
    font-size: 16px;
    color: #a3a3a3;
    margin-bottom: 40px;
    line-height: 1.5;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
  }
  .terminal {
    background: #171717;
    border: 1px solid #262626;
    border-radius: 12px;
    padding: 20px 24px;
    text-align: left;
    font-size: 13px;
    line-height: 1.8;
    animation: fadeIn 0.6s ease-out 0.4s both;
  }
  .terminal .line { color: #737373; }
  .terminal .cmd { color: #22c55e; }
  .terminal .arg { color: #60a5fa; }
  .terminal .comment { color: #525252; font-style: italic; }
  .close-hint {
    margin-top: 32px;
    font-size: 12px;
    color: #525252;
    animation: fadeIn 0.6s ease-out 0.8s both;
  }
</style>
</head>
<body>
<div class="container">
  <div class="prompt"><span style="font-weight: 700; color: #ffffff;">clictl login</span></div>
  <div style="margin-bottom: 24px; animation: fadeIn 0.6s ease-out both;">
    <svg viewBox="343 71 339 335" width="80" height="80" xmlns="http://www.w3.org/2000/svg">
      <path fill="#ffffff" d="M356.839,260.827C352.619,235.496,353.894,210.927,362.064,186.971C376.817,143.708,405.678,113.244,447.165,94.581C468.294,85.076,490.72,81.345,513.716,81.018C523.694,80.876,533.693,81.92,543.458,84.086C547.934,85.079,552.215,85.572,556.704,84.607C563.913,83.056,570.988,84.944,578.013,85.986C592.301,88.106,606.491,88.026,620.787,86.121C628.207,85.133,635.727,85.034,643.076,87.276C653.024,90.31,657.199,96.693,656.662,107.107C656.254,115.037,654.466,122.911,656.184,130.97C657.505,137.167,659.205,143.238,660.917,149.315C663.139,157.2,663.45,165.109,660.957,172.919C659.959,176.043,661.265,178.472,662.187,181.066C668.204,197.994,671.249,215.405,671.406,233.391C671.715,268.995,660.095,300.29,637.959,327.938C623.047,346.563,605.571,361.909,583.381,371.351C580.712,372.486,578.707,374.454,576.545,376.293C559.735,390.593,540.853,396.199,519.527,388.4C511.855,385.595,504.055,386.64,496.358,385.587C482.747,383.725,469.497,380.768,456.743,375.737C453.5,374.458,450.333,374.09,446.983,374.466C423.945,377.049,403.582,362.033,396.89,343.372C394.652,337.131,391.633,331.735,387.415,326.65C371.57,307.546,361.568,285.624,356.839,260.827ZM515.442,357.827C514.017,353.701,516.785,350.3,517.772,346.517C516.93,346.36,516.443,346.16,515.977,346.199C509.493,346.749,504.983,354.761,506.621,362.783C508.777,373.342,519.624,381.686,532.511,382.699C554.063,384.393,574.044,372.665,583.516,352.765C592.891,333.067,589.527,310.357,574.893,294.702C569.216,288.629,562.322,284.39,554.669,281.251C546.311,277.823,538.198,273.918,531.786,267.315C529.093,264.544,526.405,261.642,526.543,257.308C529.631,256.558,531.502,258.144,533.409,259.384C536.618,261.471,539.476,264.282,542.918,265.796C559.212,272.961,573.779,283.27,589.708,291.083C608.001,300.055,613.591,322.4,600.597,336.664C595.841,341.886,594.939,347.886,592.856,354.003C595.376,354.156,597.168,353.247,598.96,352.43C622.417,341.74,630.243,313.841,616.287,291.584C608.355,278.934,596.135,273.174,582.521,269.366C575.141,267.301,567.759,265.298,561.114,261.254C555.492,257.834,551.322,253.273,549.28,246.957C548.638,244.971,547.743,242.846,549.667,241.033C552.167,240.916,552.725,242.958,553.704,244.394C558.444,251.349,565.287,255.377,573.055,257.798C580.79,260.209,588.461,264.331,596.889,259.447C598.922,258.269,601.708,258.183,604.177,258C616.598,257.076,626.947,267.881,623.803,279.546C622.118,285.8,624.902,289.476,627.529,293.832C628.147,293.659,628.512,293.643,628.765,293.467C629.168,293.184,629.54,292.83,629.852,292.446C637.482,283.049,639.112,272.586,634.375,261.56C629.556,250.342,620.614,244.392,608.429,243.283C600.426,242.554,592.957,245.08,585.366,246.881C578.786,248.442,572.244,249.185,565.758,246.448C557.722,243.056,554.952,235.088,559.276,227.591C560.44,225.572,561.856,223.698,563.01,221.675C573.482,203.321,559.99,180.218,538.89,180.281C532.606,180.3,526.839,182.553,520.909,184.148C511.643,186.641,503.056,186.831,497.112,177.346C495.497,174.769,492.394,172.902,489.864,174.856C486.152,177.723,481.965,177.995,477.831,179.02C474.182,179.925,471.153,181.98,468.677,184.811C462.018,192.422,461.123,201.024,463.781,210.522C466.893,221.64,461.78,230.547,450.894,234.519C442.637,237.532,434.44,236.291,426.272,234.714C414.348,232.413,402.631,231.335,391.006,236.329C377.65,242.067,368.964,255.286,369.113,270.353C369.249,284.117,378.583,297.512,391.561,302.448C392.856,302.941,394.216,304.006,395.728,302.66C398.883,299.852,398.537,294.365,395.004,292.043C386.13,286.212,382.029,276.751,384.418,267.622C387.703,255.07,400.293,247.558,413.999,250.113C418.574,250.966,423.075,252.213,427.626,253.203C439.125,255.703,450.205,256.242,459.019,246.351C461.695,243.349,463.866,240.006,465.997,236.617C466.917,235.153,467.452,233.149,469.756,233.137C472.341,239.233,470.517,242.974,456.369,260.447C459.668,264.352,464.369,266.729,468.026,270.251C470.936,273.055,473.241,272.605,476.263,270.335C481.924,266.084,486.323,260.735,490.354,254.897C494.817,257.164,498.138,259.853,499.939,264.336C504.797,276.434,513.871,284.545,525.443,289.849C532.236,292.963,539.42,295.212,546.388,297.954C562.672,304.361,571.207,317.632,570.292,334.987C569.16,356.46,550.613,372.152,529.513,369.193C523.13,368.298,517.825,365.295,515.442,357.827ZM614.057,212.452C616.244,208.817,616.838,204.625,618.073,200.671C620.216,193.808,624.603,189.224,631.202,186.602C633.523,185.68,635.845,184.753,638.126,183.736C651.75,177.661,656.386,168.541,653.036,154.009C651.431,147.046,649.42,140.178,647.712,133.238C645.549,124.454,647.596,115.679,647.884,106.895C648.125,99.545,644.441,95.206,637.144,94.467C632.354,93.982,627.489,93.943,622.687,94.692C606.351,97.239,590.519,101.747,574.993,107.315C548.556,116.796,524.147,129.807,503.665,149.381C499.012,153.828,495.022,158.885,491.473,165.197C497.995,166.282,502.329,169.519,505.726,174.399C507.638,177.146,510.647,178.074,514.176,177.005C519.586,175.368,525.03,173.787,530.543,172.554C551.967,167.76,570.946,180.875,574.495,202.41C575.307,207.335,574.538,212.179,574.469,216.982C577.076,217.409,578.61,216.325,580.205,215.479C589.343,210.629,596.62,203.572,603.268,195.817C618.251,178.339,627.752,157.859,635.597,136.479C636.461,134.125,636.837,131.406,639.86,130.312C637.103,167.196,618.962,195.835,591.169,219.417C601.181,222.654,608.035,220.634,614.057,212.452ZM501.777,139.276C507.087,134.187,513.083,129.975,519.059,125.734C520.779,124.514,523.26,123.465,522.465,120.821C521.71,118.307,519.125,119.089,517.273,119.061C509.454,118.943,501.668,119.105,493.866,120.14C446.744,126.392,408.085,161.617,397.728,208.212C396.526,213.62,395.222,219.14,395.553,225.077C400.748,225.013,405.424,223.33,410.226,223.871C415.18,224.429,420.122,225.096,425.064,225.754C429.675,226.368,434.264,227.429,438.89,227.604C452.301,228.112,457.241,222.256,454.661,209.392C454.563,208.903,454.476,208.411,454.371,207.923C450.752,191.236,459.953,175.307,476.159,170.418C478.552,169.696,480.229,168.692,481.254,166.247C485.662,155.736,492.987,147.376,501.777,139.276ZM447.078,365.736C450.961,365.505,454.611,364.32,458.079,362.695C469.013,357.572,474.849,346.064,471.596,336.243C471.079,334.68,470.622,332.64,468.619,332.73C466.453,332.827,465.124,334.5,464.61,336.618C464.416,337.417,464.43,338.269,464.38,339.099C464.02,345.007,461.025,349.166,455.656,351.394C447.616,354.73,439.718,354.409,432.345,349.458C414.883,337.732,413.226,314.081,428.841,298.368C431.94,295.249,435.527,292.614,439.191,289.499C434.074,285.916,430.47,281.448,428.586,275.595C427.564,275.953,426.897,276.064,426.369,276.396C425.245,277.102,424.154,277.875,423.117,278.705C411.137,288.285,404.227,300.599,401.904,315.863C398.071,341.042,417.479,368.733,447.078,365.736ZM480.529,293.024C470.689,285.084,460.922,277.051,450.979,269.243C446.175,265.471,443.012,265.557,440.54,268.837C437.959,272.26,438.803,276.109,443.026,279.581C449.965,285.287,456.968,290.915,463.907,296.622C465.403,297.852,467.325,298.784,467.721,301.184C466.219,303.565,463.724,304.99,461.606,306.789C455.018,312.389,448.365,317.912,441.762,323.493C438.85,325.955,438.648,328.846,440.73,331.913C442.84,335.021,445.819,335.5,449.075,334.147C450.417,333.589,451.568,332.508,452.717,331.558C462.202,323.711,471.665,315.836,481.138,307.974C487.564,302.64,487.557,299.082,480.529,293.024ZM631.042,236.133C631.162,225.705,630.777,215.349,627.146,205.275C624.991,206.189,624.902,207.878,624.559,209.205C619.703,227.963,603.642,232.779,587.252,227.858C580.755,225.907,575.578,227.811,569.907,229.155C567.573,229.709,566.394,231.673,566.365,234.045C566.337,236.354,567.978,237.589,569.868,238.462C572.681,239.762,575.641,239.994,578.657,239.395C581.755,238.78,584.886,238.263,587.92,237.407C601.885,233.466,615.494,233.102,628.402,240.991C628.777,241.22,629.339,241.143,629.789,241.206C631.277,240.056,630.75,238.421,631.042,236.133ZM531.354,335.654C532.52,335.655,533.685,335.675,534.85,335.655C540.624,335.553,542.569,334.042,542.642,329.618C542.719,324.91,540.842,323.419,534.62,323.407C522.97,323.383,511.319,323.384,499.669,323.415C493.49,323.432,492.218,324.49,492.166,329.421C492.112,334.546,493.443,335.646,499.933,335.657C510.086,335.676,520.238,335.65,531.354,335.654ZM576.229,94.299C572.782,93.819,569.322,93.412,565.889,92.845C550.495,90.3,536.904,100.212,534.25,116.406C542.01,112.062,549.69,108.203,557.66,105.015C565.766,101.772,574.012,98.881,582.403,95.759C580.594,94.681,578.842,94.421,576.229,94.299ZM401.475,275.028C402.868,277.535,403.199,280.723,406.329,282.786C412.779,274.997,420.912,269.451,429.603,263.173C420.79,260.517,413.136,257.609,404.807,259.262C398.487,260.516,397.632,262.045,399.294,268.129C399.862,270.207,400.599,272.239,401.475,275.028Z"/>
      <path fill="#ffffff" d="M482.134,194.31C482.979,193.896,483.596,193.662,484.515,193.314C481.114,190.88,477.898,191.504,475.348,193.865C468.127,200.552,469.667,208.337,473.752,216.546C470.533,216.384,469.555,214.334,468.443,212.653C462.72,204.001,465.778,190.522,474.443,185.921C480.342,182.789,486.624,184.366,490.051,190.071C493.388,195.625,494.336,201.58,492.122,207.816C489.898,214.08,484.732,217.027,479.868,214.888C474.737,212.631,472.741,205.677,475.612,200.066C476.57,199.791,477.151,200.471,477.823,200.929C479.194,201.864,480.613,202.14,481.905,200.879C483.05,199.761,482.747,198.461,482.063,197.206C481.582,196.323,481.03,195.45,482.134,194.31Z"/>
      <path fill="#ffffff" d="M535.753,189.636C544.682,188.691,551.321,191.984,555.282,199.448C559.107,206.656,558.264,213.932,553.435,220.609C551.316,223.538,548.558,225.387,543.062,225.974C549.926,219.397,553.416,212.985,550.204,204.803C548.917,201.523,546.599,199.271,543.422,197.831C534.174,193.637,528.775,197.112,521.043,212.248C516.541,204.493,523.866,192.946,535.753,189.636Z"/>
      <path fill="#ffffff" d="M494.283,239.847C491.214,236.97,487.563,234.987,486.672,230.787C489.46,228.799,491.05,230.774,492.731,231.897C501.585,237.809,510.905,238.7,520.683,234.563C522.006,234.003,524.16,233.481,523.998,231.958C523.748,229.618,523.713,228.012,526.517,227.868C529.238,227.729,531.436,228.846,532.778,231.182C533.743,232.861,534.741,234.728,532.906,237.06C529.741,234.284,527.624,236.352,525.083,238.541C516.427,246.001,504.594,246.467,494.283,239.847Z"/>
      <path fill="#ffffff" d="M536.387,204.345C535.198,202.059,534.239,200.17,537.876,200.168C541.918,200.166,545.577,203.507,546.745,208.299C547.897,213.024,545.798,218.121,541.8,220.305C537.741,222.522,533.006,221.82,529.921,218.545C526.547,214.963,526.246,210.761,529.201,206.283C531.898,207.361,535.556,211.57,536.387,204.345Z"/>
      <path fill="#ffffff" d="M584.371,177.553C582.538,175.064,581.59,172.55,580.986,169.964C580.007,165.775,577.848,164.359,573.754,166.168C571.131,167.327,568.333,167.733,565.452,167.46C560.118,166.954,556.635,163.472,554.973,156.954C552.936,148.962,551.677,148.388,544.586,152.809C540.646,155.266,536.604,156.876,531.88,156.31C527.339,155.767,524.704,153.091,523.576,148.833C523.03,146.772,523.362,144.794,525.314,143.53C527.257,142.273,529.379,142.492,530.764,144.177C533.746,147.804,536.621,146.901,539.906,144.716C542.109,143.251,544.495,141.934,546.975,141.038C554.353,138.372,560.695,141.642,563.025,149.117C563.519,150.703,563.908,152.325,564.268,153.948C565.006,157.275,566.811,158.165,570.129,157.175C582.334,153.531,586.178,155.524,590.134,167.672C591.133,170.738,592.679,172.107,595.882,171.825C598.619,171.584,600.969,172.334,601.45,175.49C601.973,178.917,599.519,180.277,596.883,181.067C592.214,182.465,587.995,181.604,584.371,177.553Z"/>
    </svg>
  </div>
  <h1>Your agent is ready.</h1>
  <p class="subtitle">You have access to the full tool registry. Start building.</p>
  <div class="terminal">
    <div class="line"><span class="comment"># install a tool</span></div>
    <div class="line"><span class="cmd">clictl</span> <span class="arg">install</span> open-meteo</div>
    <div class="line" style="margin-top: 8px;"><span class="comment"># install an MCP server</span></div>
    <div class="line"><span class="cmd">clictl</span> <span class="arg">install</span> github-mcp --target claude-code</div>
    <div class="line" style="margin-top: 8px;"><span class="comment"># run a tool</span></div>
    <div class="line"><span class="cmd">clictl</span> <span class="arg">run</span> open-meteo current --latitude 37.7 --longitude -122.4</div>
  </div>
  <p class="close-hint">You can close this tab and return to your terminal.</p>
</div>
</body>
</html>`)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: handler,
	}

	go server.ListenAndServe()
	defer server.Shutdown(context.Background())

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case callback := <-callbackChan:
		if callback.Error != "" {
			return "", fmt.Errorf("OAuth error: %s", callback.Error)
		}
		if callback.State != expectedState {
			return "", fmt.Errorf("state token mismatch")
		}
		if callback.Code == "" {
			return "", fmt.Errorf("missing authorization code")
		}
		return callback.Code, nil
	}
}

// sharedMemoryEntry represents a single memory from the workspace API.
type sharedMemoryEntry struct {
	ToolName   string `json:"tool_name"`
	Note       string `json:"note"`
	MemoryType string `json:"memory_type"`
}

// syncWorkspaceMemories pulls shared memories from the active workspace and merges them locally.
func syncWorkspaceMemories(ctx context.Context, cfg *config.Config) {
	slug := cfg.Auth.ActiveWorkspace
	if slug == "" {
		return
	}

	token := config.ResolveAuthToken("", cfg)
	if token == "" {
		return
	}

	apiURL := config.ResolveAPIURL("", cfg)
	u := fmt.Sprintf("%s/api/v1/workspaces/%s/memories/", apiURL, slug)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var memories []sharedMemoryEntry
	if err := json.NewDecoder(resp.Body).Decode(&memories); err != nil {
		return
	}

	synced := 0
	for _, m := range memories {
		if m.ToolName == "" || m.Note == "" {
			continue
		}

		// Check if this memory already exists locally
		existing, _ := memory.Load(m.ToolName)
		found := false
		for _, e := range existing {
			if e.Note == m.Note {
				found = true
				break
			}
		}
		if found {
			continue
		}

		memType := memory.ParseType(m.MemoryType)
		if err := memory.Add(m.ToolName, m.Note, memType); err != nil {
			continue
		}
		synced++
	}

	if synced > 0 {
		fmt.Printf("Synced %d memories from workspace\n", synced)
	}
}

// checkCLIVersionAfterLogin checks the latest CLI version from GitHub releases
// and prints a hint to stderr if a newer version is available.
func checkCLIVersionAfterLogin(cfg *config.Config) {
	if Version == "dev" {
		return
	}

	updater.SetVersion(Version)
	latest, err := updater.ForceVersionCheck(cfg)
	if err != nil {
		return
	}

	current := strings.TrimPrefix(Version, "v")
	remote := strings.TrimPrefix(latest, "v")

	if current != remote && remote > current {
		fmt.Fprintf(os.Stderr, "A new version of clictl is available (v%s). Run 'clictl self-update' to upgrade.\n", remote)
	}
}

// storeAuthInVault stores a credential in the user vault. Initializes the
// vault key if it does not already exist. Failures are non-fatal and logged
// as warnings, since the config file also stores the credentials.
func storeAuthInVault(key, value string) {
	v := vault.NewVault(config.BaseDir())
	if !v.HasKey() {
		if err := v.InitKey(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not initialize vault key: %v\n", err)
			return
		}
	}
	if err := v.Set(key, value); err != nil {
		// Vault data is corrupted or key mismatch - reinitialize and retry
		fmt.Fprintf(os.Stderr, "Warning: vault was corrupted, reinitializing. Previously stored secrets will need to be re-added.\n")
		if reinitErr := v.InitKeyForce(); reinitErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not repair vault: %v\n", reinitErr)
			return
		}
		if retryErr := v.Set(key, value); retryErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not store %s in vault: %v\n", key, retryErr)
		}
	}
}


func init() {
	loginCmd.Flags().StringVar(&loginAPIKey, "api-key", "", "API key to authenticate with (optional; uses OAuth if not provided)")
	rootCmd.AddCommand(loginCmd)
}
