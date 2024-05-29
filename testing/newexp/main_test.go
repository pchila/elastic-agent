package newexp

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/hc-install/product"
	"github.com/hashicorp/hc-install/releases"
	"github.com/hashicorp/terraform-exec/tfexec"
	tfjson "github.com/hashicorp/terraform-json"
	"golang.org/x/crypto/ssh"
)

const flagPrefix = "integration."

type testOptions struct {
	skipDestroy      bool
	terraformWorkDir string
}

var testOpts testOptions
var pkgVar string

func init() {
	bindTestFlags(flagPrefix, flag.CommandLine, &testOpts)
}

func bindTestFlags(prefix string, flagSet *flag.FlagSet, opts *testOptions) {
	// flags
	flagSet.BoolVar(&opts.skipDestroy, prefix+"skip-destroy", false, "Set this flag to skip destroying resources")
	cwd, err := os.Getwd()
	if err != nil {
		panic(fmt.Errorf("detecting CWD: %w", err))
	}
	flagSet.StringVar(&opts.terraformWorkDir, prefix+"terraform-dir", filepath.Join(cwd, "terraform"), "Directory containing terraform files")
}

// Terraform globals
var terraformInstall *releases.ExactVersion
var terraform *tfexec.Terraform

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	flag.Parse()

	os.Exit(innerRun(ctx, m))
}

func innerRun(ctx context.Context, m *testing.M) (returnCode int) {
	defer func() {
		err := tearDown(ctx)
		if err != nil {
			log.Printf("error during teardown: %s", err)
		}
	}()

	client, err := setup(ctx)
	if err != nil {
		log.Printf("error during setup: %s", err)
		return 1
	}

	// SMALL setup for the test (this would need to be performed where the test runs)
	pkgVar = "This is not a drill."
	os.Setenv("TEST_ENV_VAR", "This is not a drill.")

	log.Printf("go test args: %s\n", os.Args)
	sb := new(strings.Builder)
	sb.WriteString("cd /src/elastic-agent && PATH=$PATH:/opt/buildkite-agent/.asdf/shims ASDF_GOLANG_VERSION=1.21.10 ASDF_DIR=/opt/buildkite-agent/.asdf/ go test")
	for _, arg := range os.Args[1:] {
		sb.WriteString(" ")
		sb.WriteString(arg)
	}

	session, err := client.NewSession()
	if err != nil {
		log.Printf("initiating ssh session: %s", err)
		return 1
	}
	defer session.Close()
	output, err := session.CombinedOutput(sb.String())
	if err != nil {
		log.Printf("error running tests on remote machine: %s", err)
		returnCode = 1
	}
	log.Printf("Test run output:\n%s\n", string(output))
	return returnCode
}

func setup(ctx context.Context) (*ssh.Client, error) {
	// Setup terraform instances
	err := prepareTerraform(ctx)
	if err != nil {
		return nil, fmt.Errorf("setup error: %w", err)
	}

	state, err := provision(ctx)

	if err != nil {
		return nil, fmt.Errorf("provisioning failed: %w", err)
	}

	// SSH into the machine to push local changes and other stuff
	privateKeyFile := state.Values.Outputs["private_key_file"].Value.(string)
	privateKeyFile = filepath.Join(testOpts.terraformWorkDir, privateKeyFile)
	pkBytes, err := os.ReadFile(privateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("reading private key file %q: %w", privateKeyFile, err)
	}
	privateKey, err := ssh.ParsePrivateKey(pkBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing private key from file %q: %w", privateKeyFile, err)
	}
	config := &ssh.ClientConfig{
		User: "buildkite-agent",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(privateKey),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	vmPublicAddress := state.Values.Outputs["vm_public_address"].Value.(string)
	// Add SSH port
	vmPublicAddress += ":22"
	client, err := ssh.Dial("tcp", vmPublicAddress, config)
	if err != nil {
		return nil, fmt.Errorf("connecting ssh %s@%s: %w", config.User, vmPublicAddress, err)
	}

	log.Printf("Connected via SSH to the machine as %s\n", client.User())

	//session, err := client.NewSession()
	//if err != nil {
	//	return nil, fmt.Errorf("initiating ssh session: %w", err)
	//}
	//defer session.Close()
	//
	//// PRE-COMMAND for setting up go and stuff
	////output, err := session.CombinedOutput("cd " + state.Values.Outputs["repo_dir"].Value.(string) + " && /opt/buildkite-agent/hooks/pre-command")
	//output, err := session.CombinedOutput(". $HOME/hooks/pre-command && asdf global golang 1.21.10")
	//log.Printf("pre-command hook output:\n%s\n", string(output))
	//if err != nil {
	//	return nil, fmt.Errorf("running pre-command hook: %w", err)
	//}
	return client, nil
}

func provision(ctx context.Context) (*tfjson.State, error) {
	err := terraform.Init(ctx, tfexec.Upgrade(true))
	if err != nil {
		return nil, fmt.Errorf("error running Init: %w", err)
	}

	planFilePath := filepath.Join(testOpts.terraformWorkDir, "main.tfplan")
	tfvarsFilePath := filepath.Join(testOpts.terraformWorkDir, "main.tfvars")
	changesRequired, err := terraform.Plan(ctx, tfexec.VarFile(tfvarsFilePath), tfexec.Out(planFilePath))
	if err != nil {
		return nil, fmt.Errorf("error running Plan: %w", err)
	}

	if !changesRequired {
		// there are no changes required
		return terraform.Show(ctx)
	}

	err = terraform.Apply(ctx, tfexec.DirOrPlan(planFilePath))
	if err != nil {
		return nil, fmt.Errorf("error running Apply: %w", err)
	}

	state, err := terraform.Show(ctx)
	if err != nil {
		return nil, fmt.Errorf("error running Show: %w", err)
	}

	return state, nil
}

func tearDown(ctx context.Context) error {
	if testOpts.skipDestroy {
		return nil
	}

	if terraform == nil {
		// terraform does not seem to be installed properly
		return fmt.Errorf("terraform does not seem to install correctly")
	}

	teardownError := terraform.Destroy(ctx, tfexec.VarFile(filepath.Join(testOpts.terraformWorkDir, "main.tfvars")))
	return errors.Join(teardownError, terraformInstall.Remove(ctx))
}

func prepareTerraform(ctx context.Context) error {
	installer := &releases.ExactVersion{
		Product: product.Terraform,
		Version: version.Must(version.NewVersion("1.8.4")),
	}
	execPath, err := installer.Install(ctx)
	if err != nil {
		return fmt.Errorf("error installing Terraform: %w", err)
	}
	terraformInstall = installer

	log.Printf("Installed terraform, exec path: %s\n", execPath)

	log.Printf("working dir: %s\n", testOpts.terraformWorkDir)

	tf, err := tfexec.NewTerraform(testOpts.terraformWorkDir, execPath)
	if err != nil {
		log.Fatalf("error running NewTerraform: %s", err)
	}
	terraform = tf

	return nil
}
