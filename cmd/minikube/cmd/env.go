/*
Copyright 2016 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.


Part of this code is heavily inspired/copied by the following file:
    github.com/docker/machine/commands/env.go
*/

package cmd

import (
    "fmt"
    "os"
	"strings"
    "text/template"

    "github.com/docker/machine/libmachine"
    "github.com/docker/machine/libmachine/shell"
    "github.com/golang/glog"
    "github.com/spf13/cobra"
    "k8s.io/minikube/pkg/minikube/cluster"
    "k8s.io/minikube/pkg/minikube/constants"
)

const (
    envTmpl = `{{ .Prefix }}DOCKER_TLS_VERIFY{{ .Delimiter }}{{ .DockerTLSVerify }}{{ .Suffix }}{{ .Prefix }}DOCKER_HOST{{ .Delimiter }}{{ .DockerHost }}{{ .Suffix }}{{ .Prefix }}DOCKER_CERT_PATH{{ .Delimiter }}{{ .DockerCertPath }}{{ .Suffix }}{{ if .NoProxyVar }}{{ .Prefix }}{{ .NoProxyVar }}{{ .Delimiter }}{{ .NoProxyValue }}{{ .Suffix }}{{end}}{{ .UsageHint }}`
)

type ShellConfig struct {
    Prefix          string
    Delimiter       string
    Suffix          string
    DockerCertPath  string
    DockerHost      string
    DockerTLSVerify string
    UsageHint       string
    NoProxyVar      string
    NoProxyValue    string
}

var no_proxy_flag bool
var shell_flag string
var unset_flag bool

func generateUsageHint(userShell string) string {

    cmd := ""
    comment := "#"
    commandLine := "minikube docker-env"

    switch userShell {
    case "fish":
        cmd = fmt.Sprintf("eval (%s)", commandLine)
    case "powershell":
        cmd = fmt.Sprintf("& %s | Invoke-Expression", commandLine)
    case "cmd":
        cmd = fmt.Sprintf("\t@FOR /f \"tokens=*\" %%i IN ('%s') DO @%%i", commandLine)
        comment = "REM"
    case "emacs":
        cmd = fmt.Sprintf("(with-temp-buffer (shell-command \"%s\" (current-buffer)) (eval-buffer))", commandLine)
        comment = ";;"
    default:
        cmd = fmt.Sprintf("eval $(%s)", commandLine)
    }

    return fmt.Sprintf("%s Run this command to configure your shell: \n%s %s\n", comment, comment, cmd)
}

func shellCfgSet(api libmachine.API) (*ShellConfig, error) {

    envMap, err := cluster.GetHostDockerEnv(api)
    if err != nil {
        return nil, err
    }

    userShell, err := getShell(shell_flag)
    if err != nil {
        return nil, err
    }

    shellCfg := &ShellConfig{
        DockerCertPath:  envMap["DOCKER_CERT_PATH"],
        DockerHost:      envMap["DOCKER_HOST"],
        DockerTLSVerify: envMap["DOCKER_TLS_VERIFY"],
        UsageHint:       generateUsageHint(userShell),
    }

    if no_proxy_flag {

        host, err := api.Load(constants.MachineName)
        if err != nil {
            return nil, fmt.Errorf("Error getting IP: ", err)
        }

        ip, err := host.Driver.GetIP()
        if err != nil {
            return nil, fmt.Errorf("Error getting host IP: %s", err)
        }

        noProxyVar, noProxyValue := findNoProxyFromEnv()

        // add the docker host to the no_proxy list idempotently
        switch {
        case noProxyValue == "":
            noProxyValue = ip
        case strings.Contains(noProxyValue, ip):
        //ip already in no_proxy list, nothing to do
        default:
            noProxyValue = fmt.Sprintf("%s,%s", noProxyValue, ip)
        }

        shellCfg.NoProxyVar = noProxyVar
        shellCfg.NoProxyValue = noProxyValue
    }

    switch userShell {
    case "fish":
        shellCfg.Prefix = "set -gx "
        shellCfg.Suffix = "\";\n"
        shellCfg.Delimiter = " \""
    case "powershell":
        shellCfg.Prefix = "$Env:"
        shellCfg.Suffix = "\"\n"
        shellCfg.Delimiter = " = \""
    case "cmd":
        shellCfg.Prefix = "SET "
        shellCfg.Suffix = "\n"
        shellCfg.Delimiter = "="
    case "emacs":
        shellCfg.Prefix = "(setenv \""
        shellCfg.Suffix = "\")\n"
        shellCfg.Delimiter = "\" \""
    default:
        shellCfg.Prefix = "export "
        shellCfg.Suffix = "\"\n"
        shellCfg.Delimiter = "=\""
    }

    return shellCfg, nil
}

func shellCfgUnset(api libmachine.API) (*ShellConfig, error) {

    userShell, err := getShell(shell_flag)
    if err != nil {
        return nil, err
    }

    shellCfg := &ShellConfig{
        UsageHint: generateUsageHint(userShell),
    }

    if no_proxy_flag {
        shellCfg.NoProxyVar, shellCfg.NoProxyValue = findNoProxyFromEnv()
    }

    switch userShell {
    case "fish":
        shellCfg.Prefix = "set -e "
        shellCfg.Suffix = ";\n"
        shellCfg.Delimiter = ""
    case "powershell":
        shellCfg.Prefix = `Remove-Item Env:\\`
        shellCfg.Suffix = "\n"
        shellCfg.Delimiter = ""
    case "cmd":
        shellCfg.Prefix = "SET "
        shellCfg.Suffix = "\n"
        shellCfg.Delimiter = "="
    case "emacs":
        shellCfg.Prefix = "(setenv \""
        shellCfg.Suffix = ")\n"
        shellCfg.Delimiter = "\" nil"
    default:
        shellCfg.Prefix = "unset "
        shellCfg.Suffix = "\n"
        shellCfg.Delimiter = ""
    }

    return shellCfg, nil
}

func executeTemplateStdout(shellCfg *ShellConfig) error {
    t := template.New("envConfig")
    tmpl, err := t.Parse(envTmpl)
    if err != nil {
        return err
    }

    return tmpl.Execute(os.Stdout, shellCfg)
}

func getShell(userShell string) (string, error) {
    if userShell != "" {
        return userShell, nil
    }
    return shell.Detect()
}

func findNoProxyFromEnv() (string, string) {
    // first check for an existing lower case no_proxy var
    noProxyVar := "no_proxy"
    noProxyValue := os.Getenv("no_proxy")

    // otherwise default to allcaps HTTP_PROXY
    if noProxyValue == "" {
        noProxyVar = "NO_PROXY"
        noProxyValue = os.Getenv("NO_PROXY")
    }
    return noProxyVar, noProxyValue
}

// envCmd represents the docker-env command
var dockerEnvCmd = &cobra.Command{
    Use:   "docker-env",
    Short: "sets up docker env variables; similar to '$(docker-machine env)'",
    Long:  `sets up docker env variables; similar to '$(docker-machine env)'`,
    Run: func(cmd *cobra.Command, args []string) {

        api := libmachine.NewClient(constants.Minipath, constants.MakeMiniPath("certs"))
        defer api.Close()

        var (
            err      error
            shellCfg *ShellConfig
        )

        if unset_flag {
            shellCfg, err = shellCfgUnset(api)
            if err != nil {
                glog.Errorln("Error setting machine env variable(s):", err)
                os.Exit(1)
            }
        } else {
            shellCfg, err = shellCfgSet(api)
            if err != nil {
                glog.Errorln("Error setting machine env variable(s):", err)
                os.Exit(1)
            }
        }

        executeTemplateStdout(shellCfg)
    },
}

func init() {

    RootCmd.AddCommand(dockerEnvCmd)
    dockerEnvCmd.Flags().BoolVar(&no_proxy_flag, "no-proxy", false, "Add machine IP to NO_PROXY environment variable")
    dockerEnvCmd.Flags().StringVar(&shell_flag, "shell", "", "Force environment to be configured for a specified shell: [fish, cmd, powershell, tcsh], default is auto-detect")
    dockerEnvCmd.Flags().BoolVarP(&unset_flag, "unset", "u", false, "Unset variables instead of setting them")
}
