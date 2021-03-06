package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

var (
	nodeConnectCommand = cli.Command{
		Name:      "connect",
		Aliases:   []string{"c"},
		Usage:     "connect to nodes / pods",
		ArgsUsage: "[pod_substring] [quorum | tessera | constellation]",
		Action: func(c *cli.Context) error {
			if c.Args().Len() < 1 {
				c.App.Run([]string{"qctl", "help", "connect", "node"})
				return cli.Exit("wrong number of arguments", 2)
			}
			namespace := c.String("namespace")
			nodeName := c.Args().First()
			container := c.Args().Get(1)
			if container == "" {
				container = "quorum"
			}
			podName := podNameFromPrefix(nodeName, namespace)
			log.Printf("trying to connect pods [%v]", podName)
			cmd := exec.Command("kubectl", "--namespace="+namespace, "exec", "-it", podName, "-c", container, "--", "/bin/ash")
			dropIntoCmd(cmd)
			return nil
		},
	}
	// qctl delete node --hard  quorum-node5
	nodeDeleteCommand = cli.Command{
		Name:  "node",
		Usage: "delete node and its associated resources (hard delete).",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "config, c",
				Usage:    "Load configuration from `FULL_PATH_FILE`",
				EnvVars:  []string{"QUBE_CONFIG"},
				Required: true,
			},
			&cli.StringFlag{ // this is only required if the user wants to delete the generated (k8s/quorum) resources as well.
				Name:    "k8sdir",
				Usage:   "The k8sdir (usually out) containing the output k8s resources",
				EnvVars: []string{"QUBE_K8S_DIR"},
			},
			&cli.BoolFlag{ // this is only required if the user wants to delete the generated (k8s/quorum) resources as well.
				Name:  "hard",
				Usage: "delete all associated resources with this node, e.g. keys, configs, etc.",
				Value: false,
			},
		},
		Action: func(c *cli.Context) error {
			if c.Args().Len() < 1 {
				c.App.Run([]string{"qctl", "help", "delete", "node"})
				return cli.Exit("wrong number of arguments", 2)
			}
			nodeName := c.Args().First()
			fmt.Println("Delete node " + nodeName)
			// TODO: abstract this away as it is used in multiple places now.
			configFile := c.String("config")
			k8sdir := c.String("k8sdir")
			isHardDelete := c.Bool("hard")
			// get the current directory path, we'll use this in case the config file passed in was a relative path.
			pwdCmd := exec.Command("pwd")
			b, _ := runCmd(pwdCmd)
			pwd := strings.TrimSpace(b.String())

			if configFile == "" {
				c.App.Run([]string{"qctl", "help", "node"})
				fmt.Println()
				fmt.Println()
				red.Println("  --config flag must be provided.")
				red.Println("             or ")
				red.Println("     QUBE_CONFIG environment variable needs to be set to your config file.")
				fmt.Println()
				red.Println(" If you need to generate a qubernetes.yaml config use the command: ")
				fmt.Println()
				green.Println("   qctl generate config")
				fmt.Println()
				return cli.Exit("--config flag must be set to the fullpath of your config file.", 3)
			}
			fmt.Println()
			green.Println("  Using config file:")
			fmt.Println()
			fmt.Println("  " + configFile)
			fmt.Println()
			fmt.Println("*****************************************************************************************")
			fmt.Println()
			// the config file must exist or this is an error.
			if fileExists(configFile) {
				// check if config file is full path or relative path.
				if !strings.HasPrefix(configFile, "/") {
					configFile = pwd + "/" + configFile
				}

			} else {
				c.App.Run([]string{"qctl", "help", "node"})
				return cli.Exit(fmt.Sprintf("ConfigFile must exist! Given configFile [%v]", configFile), 3)
			}
			configFileYaml, err := LoadYamlConfig(configFile)
			if err != nil {
				log.Fatal("config file [%v] could not be loaded into the valid qubernetes yaml. err: [%v]", configFile, err)
			}
			currentNum := len(configFileYaml.Nodes)
			fmt.Printf("config currently has %d nodes \n", currentNum)
			var nodeToDelete NodeEntry
			for i := 0; i < len(configFileYaml.Nodes); i++ {
				//displayNode(k8sdir, configFileYaml.Nodes[i], isName, isKeyDir, isConsensus, isQuorumVersion, isTmName, isTmVersion, isEnodeUrl, isQuorumImageFull)
				if configFileYaml.Nodes[i].NodeUserIdent == nodeName {
					fmt.Println("Deleting node " + nodeName)
					nodeToDelete = configFileYaml.Nodes[i]
					// try to remove the running k8s resources
					stopNode(nodeName)
					rmPersistentData(nodeName)
					rmService(nodeName)
					// TEST, if it is raft, remove it from the cluster
					if configFileYaml.Nodes[i].QuorumEntry.Quorum.Consensus == "raft" {
						// TODO: find a running node? it could either be the previous node or next node, check the index.
						// run raft.removePeer(raftId)
					}
					// Delete the resources files associated with the node, e.g. keys, k8s files, etc.
					if k8sdir != "" && isHardDelete {
						red.Println("Is hard delete remove key files and directory")
						keyDirToDelete := configFileYaml.Nodes[i].KeyDir
						nodeToDeleteKeyDir := k8sdir + "/config/" + keyDirToDelete

						// TODO: hard delete delete keys
						//rmContents := exec.Command("rm", "-f", nodeToDeleteKeyDir+"/*")
						// explicitly delete all the files that should be in the directory.
						rmContents := exec.Command("rm", "-f", nodeToDeleteKeyDir+"/acctkeyfile.json")
						dropIntoCmd(rmContents)
						rmContents = exec.Command("rm", "-f", nodeToDeleteKeyDir+"/enode")
						dropIntoCmd(rmContents)
						rmContents = exec.Command("rm", "-f", nodeToDeleteKeyDir+"/nodekey")
						dropIntoCmd(rmContents)
						rmContents = exec.Command("rm", "-f", nodeToDeleteKeyDir+"/password.txt")
						dropIntoCmd(rmContents)
						rmContents = exec.Command("rm", "-f", nodeToDeleteKeyDir+"/tm.key")
						dropIntoCmd(rmContents)
						rmContents = exec.Command("rm", "-f", nodeToDeleteKeyDir+"/tm.pub")
						dropIntoCmd(rmContents)
						// instead of running  rm -r, run rmdir on what should be an empty dir,
						// rmdir will return an error if the directory doesn't exist, so check if dir exists first.
						_, err := os.Stat(nodeToDeleteKeyDir)
						if os.IsNotExist(err) {
							log.Fatal(fmt.Sprintf("Directory does not exist, ignoring dir [%s]", nodeToDeleteKeyDir))
						} else {
							rmdir := exec.Command("rmdir", nodeToDeleteKeyDir)
							fmt.Println(rmdir)
							dropIntoCmd(rmdir)
						}

						//rmdir := exec.Command("rm", "-r", "-f", nodeToDeleteKeyDir)

					}
					// TODO: delete k8s deployment file, e.g. name: quorum-node5-quorum-deployment.yaml
					rmDeploymentFile := exec.Command("rm", "-f", k8sdir+"/deployments/"+nodeToDelete.NodeUserIdent+"-quorum-deployment.yaml")
					runCmd(rmDeploymentFile)
					// finally remove the node from the the qubernetes config, if the resources have not been delete,
					// it can be added back using the old name and it will use the keys that have not been deleted.
					configFileYaml.Nodes = append(configFileYaml.Nodes[:i], configFileYaml.Nodes[i+1:]...)
				}
			}

			// write file back
			WriteYamlConfig(configFileYaml, configFile)
			green.Println(fmt.Sprintf("  Deleted node [%s]", nodeToDelete.NodeUserIdent))
			if nodeToDelete.QuorumEntry.Quorum.Consensus == "raft" {
				green.Println(fmt.Sprintf("  This was raft node, and has not been removed from the cluster. "))
				green.Println(fmt.Sprintf("  To remove it from the current raft cluster, run on an healthy node: "))
				green.Println(fmt.Sprintf("  qctl geth exec node1 'raft.cluster'"))
				green.Println(fmt.Sprintf("  qctl geth exec node1 'raft.removePeer()'"))
			}

			return nil
		},
	}
	// qctl delete extnode  quorum-node5
	externalNodeDeleteCommand = cli.Command{
		Name:    "external-node",
		Aliases: []string{"extnode", "extnodes"},
		Usage:   "delete external node from config.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "config, c",
				Usage:    "Load configuration from `FULL_PATH_FILE`",
				EnvVars:  []string{"QUBE_CONFIG"},
				Required: true,
			},
			&cli.StringFlag{ // this is only required if the user wants to delete the generated (k8s/quorum) resources as well.
				Name:    "k8sdir",
				Usage:   "The k8sdir (usually out) containing the output k8s resources",
				EnvVars: []string{"QUBE_K8S_DIR"},
			},
		},
		Action: func(c *cli.Context) error {
			if c.Args().Len() < 1 {
				c.App.Run([]string{"qctl", "help", "delete", "node"})
				return cli.Exit("wrong number of arguments", 2)
			}
			nodeName := c.Args().First()
			fmt.Println("Delete external node " + nodeName)
			// TODO: abstract this away as it is used in multiple places now.
			configFile := c.String("config")
			//k8sdir := c.String("k8sdir")
			// get the current directory path, we'll use this in case the config file passed in was a relative path.
			pwdCmd := exec.Command("pwd")
			b, _ := runCmd(pwdCmd)
			pwd := strings.TrimSpace(b.String())

			if configFile == "" {
				c.App.Run([]string{"qctl", "help", "external-node"})
				fmt.Println()
				fmt.Println()
				red.Println("  --config flag must be provided.")
				red.Println("             or ")
				red.Println("     QUBE_CONFIG environment variable needs to be set to your config file.")
				fmt.Println()
				red.Println(" If you need to generate a qubernetes.yaml config use the command: ")
				fmt.Println()
				green.Println("   qctl generate config")
				fmt.Println()
				return cli.Exit("--config flag must be set to the fullpath of your config file.", 3)
			}
			fmt.Println()
			green.Println("  Using config file:")
			fmt.Println()
			fmt.Println("  " + configFile)
			fmt.Println()
			fmt.Println("*****************************************************************************************")
			fmt.Println()
			// the config file must exist or this is an error.
			if fileExists(configFile) {
				// check if config file is full path or relative path.
				if !strings.HasPrefix(configFile, "/") {
					configFile = pwd + "/" + configFile
				}

			} else {
				c.App.Run([]string{"qctl", "help", "external-node"})
				return cli.Exit(fmt.Sprintf("ConfigFile must exist! Given configFile [%v]", configFile), 3)
			}
			configFileYaml, err := LoadYamlConfig(configFile)
			if err != nil {
				log.Fatal("config file [%v] could not be loaded into the valid qubernetes yaml. err: [%v]", configFile, err)
			}
			currentNum := len(configFileYaml.ExternalNodes)
			fmt.Printf("config currently has %d external nodes \n", currentNum)
			fmt.Println()
			var nodeToDelete ExternalNodeEntry
			for i := 0; i < len(configFileYaml.ExternalNodes); i++ {
				//displayNode(k8sdir, configFileYaml.Nodes[i], isName, isKeyDir, isConsensus, isQuorumVersion, isTmName, isTmVersion, isEnodeUrl, isQuorumImageFull)
				if configFileYaml.ExternalNodes[i].NodeUserIdent == nodeName {
					nodeToDelete = configFileYaml.ExternalNodes[i]
					// remove the external node from the qubernetes config.
					configFileYaml.ExternalNodes = append(configFileYaml.ExternalNodes[:i], configFileYaml.ExternalNodes[i+1:]...)
					// do we want to remove the external node from the network here? or have the user run the steps separately?
					// regenerate the resource without the node
					//    > qctl generate network --k8sdir=k8sdir --update
					// deploy the new resources without the external node
					//    > qctl deploy network --k8sdir=k8sdir --wait
				}
			}
			// write file back
			WriteYamlConfig(configFileYaml, configFile)
			green.Println(fmt.Sprintf("  Deleted external node [%s] from the config", nodeToDelete.NodeUserIdent))
			green.Println("  Next Steps : ")
			green.Println("      1. regenerate the resources without the external nodes.")
			green.Println("      2. Deploy the resources to the network.")
			fmt.Print()
			green.Println("  Run the next steps by entering the commands below")
			fmt.Println("**********************************************************************************************")
			fmt.Println()
			green.Println(fmt.Sprintf("  $> qctl generate network --update"))
			green.Println(fmt.Sprintf("  $> qctl deploy network --wait"))
			fmt.Println()
			fmt.Println("**********************************************************************************************")
			return nil
		},
	}
	/*
	 * stops the give node, stopping will only remove the deployment from the K8s cluster, it will not remove any other
	 * associated resources, such as the PVC (persistent volume claim) therefore maintaining the state of the node. The
	 * services, key, and other resources are kept.
	 * The node can be restarted again, by running `qctl network create`
	 *
	 * > qctl stop node quorum-node5
	 */
	nodeStopCommand = cli.Command{
		Name:  "node",
		Usage: "stop the node(s) by deleting the associated K8s deployment.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "config, c",
				Usage:    "Load configuration from `FULL_PATH_FILE`",
				EnvVars:  []string{"QUBE_CONFIG"},
				Required: true,
			},
		},
		Action: func(c *cli.Context) error {
			if c.Args().Len() < 1 {
				c.App.Run([]string{"qctl", "help", "stop", "node"})
				return cli.Exit("wrong number of arguments", 2)
			}
			nodeName := c.Args().First()

			// TODO: abstract this away as it is used in multiple places now.
			configFile := c.String("config")
			// get the current directory path, we'll use this in case the config file passed in was a relative path.
			pwdCmd := exec.Command("pwd")
			b, _ := runCmd(pwdCmd)
			pwd := strings.TrimSpace(b.String())

			if configFile == "" {
				c.App.Run([]string{"qctl", "help", "node"})
				fmt.Println()
				fmt.Println()
				red.Println("  --config flag must be provided.")
				red.Println("             or ")
				red.Println("     QUBE_CONFIG environment variable needs to be set to your config file.")
				fmt.Println()
				red.Println(" If you need to generate a qubernetes.yaml config use the command: ")
				fmt.Println()
				green.Println("   qctl generate config")
				fmt.Println()
				return cli.Exit("--config flag must be set to the fullpath of your config file.", 3)
			}
			fmt.Println()
			green.Println("  Using config file:")
			fmt.Println()
			fmt.Println("  " + configFile)
			fmt.Println()
			fmt.Println("*****************************************************************************************")
			fmt.Println()
			// the config file must exist or this is an error.
			if fileExists(configFile) {
				// check if config file is full path or relative path.
				if !strings.HasPrefix(configFile, "/") {
					configFile = pwd + "/" + configFile
				}

			} else {
				c.App.Run([]string{"qctl", "help", "node"})
				return cli.Exit(fmt.Sprintf("ConfigFile must exist! Given configFile [%v]", configFile), 3)
			}
			configFileYaml, err := LoadYamlConfig(configFile)
			if err != nil {
				log.Fatal("config file [%v] could not be loaded into the valid qubernetes yaml. err: [%v]", configFile, err)
			}
			currentNum := len(configFileYaml.Nodes)
			fmt.Printf("config currently has %d nodes \n", currentNum)
			var nodeToStop NodeEntry
			for i := 0; i < len(configFileYaml.Nodes); i++ {
				if configFileYaml.Nodes[i].NodeUserIdent == nodeName {
					fmt.Println("Stopping node " + nodeName)
					nodeToStop = configFileYaml.Nodes[i]
					// try to remove the running k8s resources
					stopNode(nodeName)
					green.Println(fmt.Sprintf("  Stopped node [%s]", nodeToStop.NodeUserIdent))
					green.Println()
					green.Println("  to restart node run: ")
					green.Println()
					green.Println(fmt.Sprintf("    qctl deploy network"))
					green.Println()
					if nodeToStop.QuorumEntry.Quorum.Consensus == "raft" {
						green.Println(fmt.Sprintf("  This was raft node, and has not been removed from the cluster. "))
						green.Println(fmt.Sprintf("  To remove it from the current raft cluster, run on an healthy node: "))
						green.Println(fmt.Sprintf("  qctl geth exec node1 'raft.cluster'"))
						green.Println(fmt.Sprintf("  qctl geth exec node1 'raft.removePeer()'"))
					}
				}
			}
			if nodeToStop.NodeUserIdent == "" {
				fmt.Println()
				red.Println(fmt.Sprintf("  Node [%s] not found in config", nodeName))
				fmt.Println()
				red.Println(fmt.Sprintf("  To list nodes run:"))
				fmt.Println()
				red.Println("    qctl ls nodes ")
				fmt.Println()
			}

			return nil
		},
	}
	//qctl add node --id=node3 --consensus=ibft --quorum
	//TODO: get the defaults from the config file.
	nodeAddCommand = cli.Command{
		Name:      "node",
		Usage:     "add new node",
		Aliases:   []string{"n", "nodes"},
		ArgsUsage: "UniqueNodeName",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config, c",
				Usage:   "Load configuration from `FULL_PATH_FILE`",
				EnvVars: []string{"QUBE_CONFIG"},
				//Required: true,
			},
			// TODO: set default to Node-name-key-dir
			&cli.StringFlag{
				Name:  "keydir",
				Usage: "key dir where the newly generated key will be placed",
			},
			&cli.StringFlag{
				Name:  "consensus",
				Usage: "Consensus to use raft | istanbul.",
			},
			&cli.StringFlag{
				Name:    "qversion",
				Aliases: []string{"qv"},
				Usage:   "Quorum Version.",
			},
			&cli.StringFlag{
				Name:    "tmversion",
				Aliases: []string{"tmv"},
				Usage:   "Transaction Manager Version.",
			},
			&cli.StringFlag{
				Name:  "tm",
				Usage: "Transaction Manager to user: tessera | constellation.",
			},
			&cli.StringFlag{
				Name:  "qimagefull",
				Usage: "The full repo + image name of the quorum image.",
			},
		},
		Action: func(c *cli.Context) error {
			name := c.Args().First()
			// node name argument is required to update a node
			if name == "" {
				c.App.Run([]string{"qctl", "help", "node"})
				red.Println("  required argument: Unique NodeName of node you wish to add.")
				return cli.Exit("  required argument: Unique NodeName of node you wish to add.", 3)
			}
			// defaults should be obtained from the config
			keyDir := c.String("keydir")
			if keyDir == "" {
				keyDir = fmt.Sprintf("key-%s", name)
			}
			consensus := c.String("consensus")
			quorumVersion := c.String("qversion")
			tmVersion := c.String("tmversion")
			txManager := c.String("tm")
			quorumImageFull := c.String("qimagefull")

			configFile := c.String("config")

			// get the current directory path, we'll use this in case the config file passed in was a relative path.
			pwdCmd := exec.Command("pwd")
			b, _ := runCmd(pwdCmd)
			pwd := strings.TrimSpace(b.String())

			if configFile == "" {
				c.App.Run([]string{"qctl", "help", "node"})

				// QUBE_CONFIG or flag
				fmt.Println()

				fmt.Println()
				red.Println("  --config flag must be provided.")
				red.Println("             or ")
				red.Println("     QUBE_CONFIG environment variable needs to be set to your config file.")
				fmt.Println()
				red.Println(" If you need to generate a qubernetes.yaml config use the command: ")
				fmt.Println()
				green.Println("   qctl generate config")
				fmt.Println()
				return cli.Exit("--config flag must be set to the fullpath of your config file.", 3)
			}
			fmt.Println()
			green.Println("  Using config file:")
			fmt.Println()
			fmt.Println("  " + configFile)
			fmt.Println()
			fmt.Println("*****************************************************************************************")
			fmt.Println()
			// the config file must exist or this is an error.
			if fileExists(configFile) {
				// check if config file is full path or relative path.
				if !strings.HasPrefix(configFile, "/") {
					configFile = pwd + "/" + configFile
				}
			} else {
				c.App.Run([]string{"qctl", "help", "node"})
				return cli.Exit(fmt.Sprintf("ConfigFile must exist! Given configFile [%v]", configFile), 3)
			}
			configFileYaml, err := LoadYamlConfig(configFile)
			// check if the name is already taken
			for i := 0; i < len(configFileYaml.Nodes); i++ {
				nodeEntry := configFileYaml.Nodes[i]
				if name == nodeEntry.NodeUserIdent {
					red.Println(fmt.Sprintf("Node name [%s] already exist!", name))
					displayNode("", nodeEntry, true, true, true, true, true, true, false, true, true, true)
					red.Println(fmt.Sprintf("Node name [%s] exists", name))
					return cli.Exit(fmt.Sprintf("Node name [%s] exists, node names must be unique", name), 3)
				}
			}
			if err != nil {
				log.Fatal("config file [%v] could not be loaded into the valid qubernetes yaml. err: [%v]", configFile, err)
			}
			// set defaults from the existing config if node values were not provided
			if quorumVersion == "" {
				quorumVersion = configFileYaml.Genesis.QuorumVersion
			}
			if consensus == "" {
				consensus = configFileYaml.Genesis.Consensus
			}
			// for the transaction manager, set the defaults to what is available on the first node.
			if txManager == "" {
				txManager = configFileYaml.Nodes[0].QuorumEntry.Tm.Name
			}
			if tmVersion == "" {
				tmVersion = configFileYaml.Nodes[0].QuorumEntry.Tm.TmVersion
			}
			fmt.Println(fmt.Sprintf("Adding node [%s] key dir [%s]", name, keyDir))
			currentNum := len(configFileYaml.Nodes)
			fmt.Println(fmt.Sprintf("config currently has %d nodes", currentNum))
			nodeEntry := createNodeEntry(name, keyDir, consensus, quorumVersion, txManager, tmVersion, quorumImageFull)
			configFileYaml.Nodes = append(configFileYaml.Nodes, nodeEntry)
			fmt.Println()
			green.Println("Adding Node: ")
			displayNode("", nodeEntry, true, true, true, true, true, true, false, true, true, true)
			// write file back
			WriteYamlConfig(configFileYaml, configFile)
			fmt.Println("The node(s) have been added to the config file [%s]", configFile)
			fmt.Println("Next, generate (update) the additional node resources for quorum and k8s:")
			fmt.Println()
			fmt.Println("**********************************************************************************************")
			fmt.Println()
			green.Println(fmt.Sprintf("  $> qctl generate network --update"))
			fmt.Println()
			fmt.Println("**********************************************************************************************")

			return nil
		},
	}
	//qctl add extnode --enode="enode://12343@1.2.3.4:7000" --tmurl=http://1.2.3.4:9000  --nodekeyaddr=0x1234334
	externalNodeAddCommand = cli.Command{
		Name:      "external-node",
		Usage:     "add new external node",
		Aliases:   []string{"extnode", "extnodes", "external-nodes"},
		ArgsUsage: "UniqueNodeName",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config, c",
				Usage:   "Load configuration from `FULL_PATH_FILE`",
				EnvVars: []string{"QUBE_CONFIG"},
			},
			// TODO: set default to Node-name-key-dir
			&cli.StringFlag{
				Name:     "enode",
				Aliases:  []string{"enodeurl"},
				Usage:    "enode url of the external node to add (p2p portion must be reachable).",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "tmurl",
				Aliases:  []string{"tm"},
				Usage:    "transaction manager url of the external node to add (must be reachable).",
				Required: true,
			},
			&cli.StringFlag{
				Name:    "nodekeyaddr",
				Aliases: []string{"nkaddr", "na"},
				Usage:   "Nodekey address required for ibft only.",
			},
		},
		Action: func(c *cli.Context) error {
			name := c.Args().First()
			// node name argument is required to update a node
			if name == "" {
				c.App.Run([]string{"qctl", "help", "external-node"})
				red.Println("  required argument: Unique NodeName of the external node you wish to add.")
				return cli.Exit("  required argument: Unique NodeName of external node you wish to add.", 3)
			}

			enode := c.String("enode")
			tmurl := c.String("tmurl")
			nodekeyaddr := c.String("nodekeyaddr")

			configFile := c.String("config")
			if configFile == "" {
				c.App.Run([]string{"qctl", "help", "external-node"})

				// QUBE_CONFIG or flag
				fmt.Println()

				fmt.Println()
				red.Println("  --config flag must be provided.")
				red.Println("             or ")
				red.Println("     QUBE_CONFIG environment variable needs to be set to your config file.")
				fmt.Println()
				red.Println(" If you need to generate a qubernetes.yaml config use the command: ")
				fmt.Println()
				green.Println("   qctl generate config")
				fmt.Println()
				return cli.Exit("--config flag must be set to the fullpath of your config file.", 3)
			}
			fmt.Println()
			green.Println("  Using config file:")
			fmt.Println()
			fmt.Println("  " + configFile)
			fmt.Println()
			fmt.Println("*****************************************************************************************")
			fmt.Println()

			// get the current directory path, we'll use this in case the config file passed in was a relative path.
			pwdCmd := exec.Command("pwd")
			b, _ := runCmd(pwdCmd)
			pwd := strings.TrimSpace(b.String())
			// the config file must exist or this is an error.
			if fileExists(configFile) {
				// check if config file is full path or relative path.
				if !strings.HasPrefix(configFile, "/") {
					configFile = pwd + "/" + configFile
				}

			} else {
				c.App.Run([]string{"qctl", "help", "external-node"})
				return cli.Exit(fmt.Sprintf("ConfigFile must exist! Given configFile [%v]", configFile), 3)
			}
			configFileYaml, err := LoadYamlConfig(configFile)
			// check if the name is already taken
			for i := 0; i < len(configFileYaml.ExternalNodes); i++ {
				externalNode := configFileYaml.ExternalNodes[i]
				if name == externalNode.NodeUserIdent {
					red.Println(fmt.Sprintf("External node name [%s] already exist!", name))
					displayExternalNode(externalNode, true, true, true, true)
					red.Println(fmt.Sprintf("External node name [%s] exists", name))
					return cli.Exit(fmt.Sprintf("External node name [%s] exists, External node names must be unique", name), 3)
				}
			}
			if err != nil {
				log.Fatal("config file [%v] could not be loaded into the valid qubernetes yaml. err: [%v]", configFile, err)
			}

			fmt.Println(fmt.Sprintf("Adding external node [%s] ", name))
			currentNumExtNodes := len(configFileYaml.ExternalNodes)
			fmt.Println(fmt.Sprintf("config currently has %d nodes", currentNumExtNodes))
			externalNodeEntry := ExternalNodeEntry{
				NodeUserIdent: name,
				EnodeUrl:      enode,
				TmUrl:         tmurl,
			}
			if nodekeyaddr != "" {
				externalNodeEntry.NodekeyAddress = nodekeyaddr
			}
			configFileYaml.ExternalNodes = append(configFileYaml.ExternalNodes, externalNodeEntry)
			fmt.Println()
			green.Println("Adding External Node: ")
			displayExternalNode(externalNodeEntry, true, true, true, true)
			// write file back
			WriteYamlConfig(configFileYaml, configFile)
			fmt.Println("The external node(s) have been added to the config file [%s]", configFile)
			fmt.Println("Next, generate (update) the additional node resources for quorum and k8s:")
			fmt.Println()
			fmt.Println("**********************************************************************************************")
			fmt.Println()
			green.Println(fmt.Sprintf("  $> qctl generate network --update"))
			fmt.Println()
			fmt.Println("**********************************************************************************************")

			return nil
		},
	}
	// TODO: consolidate this and add node
	nodeUpdateCommand = cli.Command{
		Name:      "node",
		Usage:     "update node",
		Aliases:   []string{"n", "nodes"},
		ArgsUsage: "NodeName",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config, c",
				Usage:   "Load configuration from `FULL_PATH_FILE`",
				EnvVars: []string{"QUBE_CONFIG"},
				//Required: true,
			},
			// TODO: set default to Node-name-key-dir
			&cli.StringFlag{
				Name:  "keydir",
				Usage: "key dir where the newly generated key will be placed",
			},
			&cli.StringFlag{
				Name:  "consensus",
				Usage: "Consensus to use raft | istanbul.",
			},
			&cli.StringFlag{
				Name:    "qversion",
				Aliases: []string{"qv"},
				Usage:   "Quorum Version.",
			},
			&cli.StringFlag{
				Name:    "tmversion",
				Aliases: []string{"tmv"},
				Usage:   "Transaction Manager Version.",
			},
			&cli.StringFlag{
				Name:  "tm",
				Usage: "Transaction Manager to user: tessera | constellation.",
			},
			&cli.StringFlag{
				Name:  "qimagefull",
				Usage: "The full repo + image name of the quorum image.",
			},
			&cli.StringFlag{
				Name:  "tmimagefull",
				Usage: "The full repo + image name of the tm image.",
			},
			&cli.StringFlag{
				Name:  "gethparams",
				Usage: "Set the geth startup params.",
			},
		},
		Action: func(c *cli.Context) error {
			name := c.Args().First()
			// node name argument is required to update a node
			if name == "" {
				c.App.Run([]string{"qctl", "help", "node"})
				red.Println("  NodeName required to update a node.")
				return cli.Exit("  NodeName required to update a node.", 3)
			}
			// defaults should be obtained from the config
			keyDir := c.String("keydir")
			if keyDir == "" {
				keyDir = fmt.Sprintf("key-%s", name)
			}
			consensus := c.String("consensus")
			quorumVersion := c.String("qversion")
			tmVersion := c.String("tmversion")
			txManager := c.String("tm")
			quorumImageFull := c.String("qimagefull")
			tmImageFull := c.String("tmimagefull")
			gethparams := c.String("gethparams")
			configFile := c.String("config")

			// get the current directory path, we'll use this in case the config file passed in was a relative path.
			pwdCmd := exec.Command("pwd")
			b, _ := runCmd(pwdCmd)
			pwd := strings.TrimSpace(b.String())

			if configFile == "" {
				c.App.Run([]string{"qctl", "help", "node"})

				// QUBE_CONFIG or flag
				fmt.Println()

				fmt.Println()
				red.Println("  --config flag must be provided.")
				red.Println("             or ")
				red.Println("     QUBE_CONFIG environment variable needs to be set to your config file.")
				fmt.Println()
				red.Println(" If you need to generate a qubernetes.yaml config use the command: ")
				fmt.Println()
				green.Println("   qctl generate config")
				fmt.Println()
				return cli.Exit("--config flag must be set to the fullpath of your config file.", 3)
			}
			fmt.Println()
			green.Println("  Using config file:")
			fmt.Println()
			fmt.Println("  " + configFile)
			fmt.Println()
			fmt.Println("*****************************************************************************************")
			fmt.Println()
			// the config file must exist or this is an error.
			if fileExists(configFile) {
				// check if config file is full path or relative path.
				if !strings.HasPrefix(configFile, "/") {
					configFile = pwd + "/" + configFile
				}

			} else {
				c.App.Run([]string{"qctl", "help", "node"})
				return cli.Exit(fmt.Sprintf("ConfigFile must exist! Given configFile [%v]", configFile), 3)
			}
			configFileYaml, err := LoadYamlConfig(configFile)
			if err != nil {
				log.Fatal("config file [%v] could not be loaded into the valid qubernetes yaml. err: [%v]", configFile, err)
			}
			// find the nodes
			var updatedNode NodeEntry
			for i := 0; i < len(configFileYaml.Nodes); i++ {
				nodeEntry := configFileYaml.Nodes[i]
				if name == nodeEntry.NodeUserIdent {
					displayNode("", nodeEntry, true, true, true, true, true, true, false, true, true, true)
					if gethparams != "" {
						nodeEntry.GethEntry.GetStartupParams = gethparams
					}
					if quorumImageFull != "" {
						nodeEntry.QuorumEntry.Quorum.DockerRepoFull = quorumImageFull
					}
					if tmImageFull != "" {
						nodeEntry.QuorumEntry.Tm.DockerRepoFull = tmImageFull
					}
					if quorumVersion != "" {
						nodeEntry.QuorumEntry.Quorum.QuorumVersion = quorumVersion
					}
					if tmVersion != "" {
						nodeEntry.QuorumEntry.Tm.TmVersion = tmVersion
					}
					if txManager != "" {
						nodeEntry.QuorumEntry.Tm.Name = txManager
					}
					if consensus != "" {
						nodeEntry.QuorumEntry.Quorum.Consensus = consensus
					}
					updatedNode = nodeEntry
					configFileYaml.Nodes[i] = updatedNode
				}
			}
			// If the node name the user entered to update does not exists, error out and notify the user.
			if updatedNode.NodeUserIdent == "" {
				red.Println(fmt.Sprintf("Node name [%s] does not exist, not updating any nodes.", name))
				fmt.Println()
				red.Println("to see current nodes run: ")
				fmt.Println()
				red.Println("  qctl ls nodes")
				fmt.Println()
				return cli.Exit(fmt.Sprintf("node name doesn't exist [%s]", name), 3)
			}
			fmt.Println(fmt.Sprintf("Updating node [%s] key dir [%s]", name, keyDir))
			fmt.Println()
			green.Println("Updating Node: ")
			displayNode("", updatedNode, true, true, true, true, true, true, false, true, true, true)
			// write file back
			WriteYamlConfig(configFileYaml, configFile)
			fmt.Println("The node have been updated the config file [%s]", configFile)
			fmt.Println("Next, generate (update) the additional node resources for quorum and k8s:")
			fmt.Println()
			fmt.Println("**********************************************************************************************")
			fmt.Println()
			green.Println(fmt.Sprintf("  $> qctl generate network --update"))
			fmt.Println()
			fmt.Println("**********************************************************************************************")

			return nil
		},
	}
	// qctl ls node --name --consensus --quorumversion
	// qctl ls node --name --consensus --quorumversion --tmversion --tmname
	// qctl ls nodes --asexternal -b --node-ip=$(minikube ip)
	nodeListCommand = cli.Command{
		Name:      "node",
		Usage:     "list nodes info",
		Aliases:   []string{"n", "nodes"},
		ArgsUsage: "NodeName",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "config, c",
				Usage:    "Load configuration from `FULL_PATH_FILE`",
				EnvVars:  []string{"QUBE_CONFIG"},
				Required: true,
			},
			&cli.StringFlag{ // this is only required to get the enodeurl
				Name:    "k8sdir",
				Usage:   "The k8sdir (usually out) containing the output k8s resources",
				EnvVars: []string{"QUBE_K8S_DIR"},
			},
			&cli.BoolFlag{
				Name:  "all",
				Usage: "display all node values",
			},
			&cli.BoolFlag{
				Name:  "name",
				Usage: "display the name of the node",
			},
			&cli.BoolFlag{
				Name:  "consensus",
				Usage: "display the consensus of the node",
			},
			&cli.BoolFlag{
				Name:  "quorumversion",
				Usage: "display the quorumversion of the node",
			},
			&cli.StringFlag{
				Name:  "qimagefull",
				Usage: "The full repo + image name of the quorum image",
			},
			&cli.StringFlag{
				Name:  "tmimagefull",
				Usage: "The full repo + image name of the tm image",
			},
			&cli.BoolFlag{
				Name:  "tmname",
				Usage: "display the tm name of the node",
			},
			&cli.BoolFlag{
				Name:  "tmversion",
				Usage: "display the tm version of the node",
			},
			&cli.BoolFlag{
				Name:  "keydir",
				Usage: "display the keydir of the node",
			},
			&cli.BoolFlag{
				Name:    "enodeurl",
				Aliases: []string{"enode"},
				Usage:   "display the enodeurl of the node",
			},
			&cli.BoolFlag{
				Name:    "gethparams",
				Aliases: []string{"gp"},
				Usage:   "display the geth startup params of the node",
			},
			&cli.BoolFlag{
				Name:    "asexternal",
				Aliases: []string{"asext"},
				Usage:   "display information necessary for sending to another cluster for setup",
			},
			&cli.StringFlag{
				Name:  "node-ip",
				Usage: "the IP of the K8s node, e.g. minikube ip (used with asexternal).",
				Value: "<K8s_NODE_IP>",
			},
			&cli.BoolFlag{
				Name:    "bare",
				Aliases: []string{"b"},
				Usage:   "display the minimum output, useful for scripts / automation",
			},
		},
		Action: func(c *cli.Context) error {
			// potentially show only this node
			nodeName := c.Args().First()
			namespace := c.String("namespace")
			nodeFound := true
			if nodeName != "" { // if the user request a specific node, we want to make sure we let them know it was found or not.
				nodeFound = false
			}
			isName := c.Bool("name")
			isConsensus := c.Bool("consensus")
			isQuorumVersion := c.Bool("quorumversion")
			isTmName := c.Bool("tmname")
			isTmVersion := c.Bool("tmversion")
			isKeyDir := c.Bool("keydir")
			isEnodeUrl := c.Bool("enodeurl")
			isQuorumImageFull := c.Bool("qimagefull")
			isTmImageFull := c.Bool("tmimagefull")
			isGethParams := c.Bool("gethparams")
			isAll := c.Bool("all")
			isBare := c.Bool("bare")
			k8sdir := c.String("k8sdir")
			// display node info for external cluster.
			asExternal := c.Bool("asexternal")
			nodeip := c.String("node-ip")

			configFile := c.String("config")
			// set all values to true
			if isAll {
				isName = true
				isConsensus = true
				isQuorumVersion = true
				isTmName = true
				isTmVersion = true
				if k8sdir != "" {
					isEnodeUrl = true
				}
				isQuorumImageFull = true
				isTmImageFull = true
				isGethParams = true
			}

			if configFile == "" {
				c.App.Run([]string{"qctl", "help", "node"})

				// QUBE_CONFIG or flag
				fmt.Println()

				fmt.Println()
				red.Println("  --config flag must be provided.")
				red.Println("             or ")
				red.Println("     QUBE_CONFIG environment variable needs to be set to your config file.")
				fmt.Println()
				red.Println(" If you need to generate a qubernetes.yaml config use the command: ")
				fmt.Println()
				green.Println("   qctl generate config")
				fmt.Println()
				return cli.Exit("--config flag must be set to the fullpath of your config file.", 3)
			}

			// get the current directory path, we'll use this in case the config file passed in was a relative path.
			pwdCmd := exec.Command("pwd")
			b, _ := runCmd(pwdCmd)
			pwd := strings.TrimSpace(b.String())

			// the config file must exist or this is an error.
			if fileExists(configFile) {
				// check if config file is full path or relative path.
				if !strings.HasPrefix(configFile, "/") {
					configFile = pwd + "/" + configFile
				}

			} else {
				c.App.Run([]string{"qctl", "help", "node"})
				return cli.Exit(fmt.Sprintf("ConfigFile must exist! Given configFile [%v]", configFile), 3)
			}
			if !isBare {
				fmt.Println()
				green.Println("  Using config file:")
				fmt.Println()
				fmt.Println("  " + configFile)
				fmt.Println()
				if k8sdir != "" {
					green.Println("  K8sdir set to:")
					fmt.Println()
					fmt.Println("  " + k8sdir)
					fmt.Println()
				}
				fmt.Println("*****************************************************************************************")
				fmt.Println()
			}

			configFileYaml, err := LoadYamlConfig(configFile)
			if err != nil {
				log.Fatal("config file [%v] could not be loaded into the valid qubernetes yaml. err: [%v]", configFile, err)
			}
			currentNum := len(configFileYaml.Nodes)
			if !isBare {
				fmt.Printf("config currently has %d nodes \n", currentNum)
			}

			if asExternal {
				fmt.Println("external_nodes:")
			}

			for i := 0; i < len(configFileYaml.Nodes); i++ {
				currentNode := configFileYaml.Nodes[i]
				if nodeName == currentNode.NodeUserIdent || nodeName == "" { // node name not set always show node
					nodeFound = true
					if asExternal { // qctl ls nodes --asexternal -b --node-ip=$(minikube ip)

						// qctl ls urls --node quorum-node1 --tm -bare
						tmUrlCmd := exec.Command("qctl", "ls", "urls", "--node="+currentNode.NodeUserIdent, "--type=nodeport", "--tm", "--bare", "--node-ip="+nodeip)
						//fmt.Println(cmd.String())
						res, err := runCmd(tmUrlCmd)
						if err != nil {
							log.Fatal(err)
						}
						tmUrl := strings.TrimSpace(res.String())

						p2pCmd := exec.Command("qctl", "--namespace="+namespace, "ls", "urls", "--node="+currentNode.NodeUserIdent, "--type=nodeport", "--p2p", "--bare", "--node-ip="+nodeip)
						//fmt.Println(p2pCmd.String())
						res, err = runCmd(p2pCmd)
						if err != nil {
							log.Fatal(err)
						}
						p2pUrl := strings.TrimSpace(res.String())

						// kc get configMap quorum-node1-nodekey-address-config -o jsonpath='{.data.nodekey}'
						// try to get the node key address (ibft)
						nodeKeyAddrCmd := exec.Command("kubectl", "get", "configMap",
							currentNode.NodeUserIdent+"-nodekey-address-config", "-o=jsonpath='{.data.nodekey}'", "--namespace="+namespace)
						res, err = runCmd(nodeKeyAddrCmd)
						nodekeyAddress := ""
						if err != nil && currentNode.QuorumEntry.Quorum.Consensus == IstanbulConsensus {
							fmt.Println("namespace", namespace)
							red.Println(fmt.Sprintf(" issue getting the nodekey-address for node %s", configFileYaml.Nodes[i].NodeUserIdent))
							red.Println(nodeKeyAddrCmd.String())
							log.Fatal(err)
						} else {
							nodekeyAddress = strings.ReplaceAll(res.String(), "'", "")
							nodekeyAddress = strings.TrimSpace(nodekeyAddress)
						}
						//fmt.Println("nodekeyAddress", nodekeyAddress)
						displayNodeAsExternal(k8sdir, currentNode, p2pUrl, tmUrl, nodekeyAddress, true)
					} else {
						if isBare { // show the bare version, cleaner for scripts.
							displayNodeBare(k8sdir, currentNode, isName, isKeyDir, isConsensus, isQuorumVersion, isTmName, isTmVersion, isEnodeUrl, isQuorumImageFull, isTmImageFull, isGethParams)
						} else {
							displayNode(k8sdir, currentNode, isName, isKeyDir, isConsensus, isQuorumVersion, isTmName, isTmVersion, isEnodeUrl, isQuorumImageFull, isTmImageFull, isGethParams)
						}
					}
				}
			}
			// if the nodename was specified, but not found in the config, list the names of the nodes for the user.
			if !nodeFound {
				fmt.Println()
				red.Println(fmt.Sprintf("  Node name [%s] not found in config file ", nodeName))
				fmt.Println()
				fmt.Println(fmt.Sprintf("  Node Names are: "))
				for i := 0; i < len(configFileYaml.Nodes); i++ {
					fmt.Println(fmt.Sprintf("    [%s]", configFileYaml.Nodes[i].NodeUserIdent))
				}
			}
			return nil
		},
	}
	externalNodeListCommand = cli.Command{
		Name:      "external-node",
		Usage:     "list external node(s) info",
		Aliases:   []string{"extnode", "extnodes", "external-nodes"},
		ArgsUsage: "NodeName",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "config, c",
				Usage:    "Load configuration from `FULL_PATH_FILE`",
				EnvVars:  []string{"QUBE_CONFIG"},
				Required: true,
			},
			&cli.BoolFlag{
				Name:  "all",
				Usage: "display all node values",
			},
			&cli.BoolFlag{
				Name:  "name",
				Usage: "display the name of the node",
			},
			&cli.BoolFlag{
				Name:    "enodeurl",
				Aliases: []string{"enode"},
				Usage:   "display the enodeurl of the external node",
			},
			&cli.BoolFlag{
				Name:  "tmurl",
				Usage: "display the transaction manager url for the external node",
			}, //Node_Acct_Addr
			&cli.BoolFlag{
				Name:  "nodekey-addr",
				Usage: "display the nodekey address for the external node (ibft)",
			},
			&cli.BoolFlag{
				Name:    "bare",
				Aliases: []string{"b"},
				Usage:   "display the minimum output, useful for scripts / automation",
			},
		},
		Action: func(c *cli.Context) error {
			// potentially show only this node
			nodeName := c.Args().First()
			nodeFound := true
			if nodeName != "" { // if the user request a specific node, we want to make sure we let them know it was found or not.
				nodeFound = false
			}
			isName := c.Bool("name")
			isEnodeUrl := c.Bool("enodeurl")
			isTmUrl := c.Bool("tmurl")
			isNodeKeyAddr := c.Bool("nodekey-addr")
			isAll := c.Bool("all")
			isBare := c.Bool("bare")

			configFile := c.String("config")
			// if no flags are set, show the names
			if !isName && !isEnodeUrl && !isTmUrl && !isNodeKeyAddr && !isAll {
				isAll = true
			}
			// set all values to true
			if isAll {
				isName = true
				isEnodeUrl = true
				isTmUrl = true
				isNodeKeyAddr = true
			}

			if configFile == "" {
				c.App.Run([]string{"qctl", "help", "node"})

				// QUBE_CONFIG or flag
				fmt.Println()

				fmt.Println()
				red.Println("  --config flag must be provided.")
				red.Println("             or ")
				red.Println("     QUBE_CONFIG environment variable needs to be set to your config file.")
				fmt.Println()
				red.Println(" If you need to generate a qubernetes.yaml config use the command: ")
				fmt.Println()
				green.Println("   qctl generate config")
				fmt.Println()
				return cli.Exit("--config flag must be set to the fullpath of your config file.", 3)
			}

			// get the current directory path, we'll use this in case the config file passed in was a relative path.
			pwdCmd := exec.Command("pwd")
			b, _ := runCmd(pwdCmd)
			pwd := strings.TrimSpace(b.String())

			// the config file must exist or this is an error.
			if fileExists(configFile) {
				// check if config file is full path or relative path.
				if !strings.HasPrefix(configFile, "/") {
					configFile = pwd + "/" + configFile
				}
			} else {
				c.App.Run([]string{"qctl", "help", "external-node"})
				return cli.Exit(fmt.Sprintf("ConfigFile must exist! Given configFile [%v]", configFile), 3)
			}
			if !isBare {
				fmt.Println()
				green.Println("  Using config file:")
				fmt.Println()
				fmt.Println("  " + configFile)
				fmt.Println()
				fmt.Println("*****************************************************************************************")
				fmt.Println()
			}

			configFileYaml, err := LoadYamlConfig(configFile)
			if err != nil {
				log.Fatal("config file [%v] could not be loaded into the valid qubernetes yaml. err: [%v]", configFile, err)
			}
			currentNum := len(configFileYaml.Nodes)
			if !isBare {
				fmt.Printf("config currently has %d external nodes \n", currentNum)
				fmt.Println()
				fmt.Println("external_nodes:")
			}

			for i := 0; i < len(configFileYaml.ExternalNodes); i++ {
				currentExternalNode := configFileYaml.ExternalNodes[i]
				if nodeName == currentExternalNode.NodeUserIdent || nodeName == "" { // node name not set always show node
					nodeFound = true
					if isBare {
						displayExternalNodeBare(currentExternalNode, isName, isEnodeUrl, isTmUrl, isNodeKeyAddr)
					} else {
						displayExternalNode(currentExternalNode, isName, isEnodeUrl, isTmUrl, isNodeKeyAddr)
					}
				}
			}
			// if the nodename was specified, but not found in the config, list the names of the nodes for the user.
			if !nodeFound {
				fmt.Println()
				red.Println(fmt.Sprintf("  External node name [%s] not found in config file ", nodeName))
				fmt.Println()
				fmt.Println(fmt.Sprintf("  External Node Names are: "))
				for i := 0; i < len(configFileYaml.ExternalNodes); i++ {
					fmt.Println(fmt.Sprintf("    [%s]", configFileYaml.ExternalNodes[i].NodeUserIdent))
				}
			}
			return nil
		},
	}
)

func createNodeEntry(nodeName, nodeKeyDir, consensus, quorumVersion, txManager, tmVersion, quorumImageFull string) NodeEntry {
	quorum := Quorum{
		Consensus:      consensus,
		QuorumVersion:  quorumVersion,
		DockerRepoFull: quorumImageFull,
	}
	tm := Tm{
		Name:      txManager,
		TmVersion: tmVersion,
	}
	quorumEntry := QuorumEntry{
		Quorum: quorum,
		Tm:     tm,
	}
	nodeEntry := NodeEntry{
		NodeUserIdent: nodeName,
		KeyDir:        nodeKeyDir,
		QuorumEntry:   quorumEntry,
	}
	return nodeEntry
}

// QUBE_K8S_DIR
// cat $QUBE_K8S_DIR/config/permissioned-nodes.json | grep quorum-node1
func getEnodeUrl(nodeName, qubeK8sDir string) string {
	c1 := exec.Command("cat", qubeK8sDir+"/config/permissioned-nodes.json")
	c2 := exec.Command("grep", nodeName)

	r, w := io.Pipe()
	c1.Stdout = w
	c2.Stdin = r

	var out bytes.Buffer
	c2.Stdout = &out
	c1.Start()
	c2.Start()
	c1.Wait()
	w.Close()
	c2.Wait()
	enodeUrl := strings.TrimSpace(out.String())
	enodeUrl = strings.ReplaceAll(enodeUrl, ",", "")
	return enodeUrl
}

func displayNode(k8sdir string, nodeEntry NodeEntry, name, consensus, keydir, quorumVersion, txManager, tmVersion, isEnodeUrl, isQuorumImageFull, isTmImageFull, isGethParms bool) {
	fmt.Println()
	green.Println(fmt.Sprintf("     [%s] unique name", nodeEntry.NodeUserIdent))
	if keydir {
		green.Println(fmt.Sprintf("     [%s] keydir: [%s]", nodeEntry.NodeUserIdent, nodeEntry.KeyDir))
	}
	if consensus {
		green.Println(fmt.Sprintf("     [%s] consensus: [%s]", nodeEntry.NodeUserIdent, nodeEntry.QuorumEntry.Quorum.Consensus))
	}
	if quorumVersion {
		green.Println(fmt.Sprintf("     [%s] quorumVersion: [%s]", nodeEntry.NodeUserIdent, nodeEntry.QuorumEntry.Quorum.QuorumVersion))
	}
	if txManager {
		green.Println(fmt.Sprintf("     [%s] txManager: [%s]", nodeEntry.NodeUserIdent, nodeEntry.QuorumEntry.Tm.Name))
	}
	if tmVersion {
		green.Println(fmt.Sprintf("     [%s] tmVersion: [%s]", nodeEntry.NodeUserIdent, nodeEntry.QuorumEntry.Tm.TmVersion))
	}
	if isQuorumImageFull && nodeEntry.QuorumEntry.Quorum.DockerRepoFull != "" {
		green.Println(fmt.Sprintf("     [%s] quorumImage: [%s]", nodeEntry.NodeUserIdent, nodeEntry.QuorumEntry.Quorum.DockerRepoFull))
	}
	if isTmImageFull && nodeEntry.QuorumEntry.Tm.DockerRepoFull != "" {
		green.Println(fmt.Sprintf("     [%s] tmImage: [%s]", nodeEntry.NodeUserIdent, nodeEntry.QuorumEntry.Tm.DockerRepoFull))
	}
	if isEnodeUrl {
		if k8sdir == "" {
			red.Println("Set --k8sdir flag or QUBE_K8S_DIR env in order to display enodeurl")
		} else {
			enodeUrl := getEnodeUrl(nodeEntry.NodeUserIdent, k8sdir)
			if enodeUrl != "" {
				green.Println(fmt.Sprintf("     [%s] enodeUrl: [%s]", nodeEntry.NodeUserIdent, enodeUrl))
			}
		}
	}
	if isGethParms && nodeEntry.GethEntry.GetStartupParams != "" {
		green.Println(fmt.Sprintf("     [%s] geth params: [%s]", nodeEntry.NodeUserIdent, nodeEntry.GethEntry.GetStartupParams))
	}
	fmt.Println()
}

func displayNodeBare(k8sdir string, nodeEntry NodeEntry, name, consensus, keydir, quorumVersion, txManager, tmVersion, isEnodeUrl, isQuorumImageFull, isTmImageFull, isGethParms bool) {
	if name {
		fmt.Println(nodeEntry.NodeUserIdent)
	}
	if keydir {
		fmt.Println(nodeEntry.KeyDir)
	}
	if consensus {
		fmt.Println(nodeEntry.QuorumEntry.Quorum.Consensus)
	}
	if quorumVersion {
		fmt.Println(nodeEntry.QuorumEntry.Quorum.QuorumVersion)
	}
	if txManager {
		fmt.Println(nodeEntry.QuorumEntry.Tm.Name)
	}
	if tmVersion {
		fmt.Println(nodeEntry.QuorumEntry.Tm.TmVersion)
	}
	if isQuorumImageFull {
		fmt.Println(nodeEntry.QuorumEntry.Quorum.DockerRepoFull)
	}
	if isTmImageFull {
		fmt.Println(nodeEntry.QuorumEntry.Tm.DockerRepoFull)
	}
	if isEnodeUrl {
		if k8sdir == "" {
			red.Println("Set --k8sdir flag or QUBE_K8S_DIR env in order to display enodeurl")
		} else {
			enodeUrl := getEnodeUrl(nodeEntry.NodeUserIdent, k8sdir)
			fmt.Println(enodeUrl)
		}
	}
	if isGethParms {
		fmt.Println(nodeEntry.GethEntry.GetStartupParams)
	}
}

func displayNodeAsExternal(k8sdir string, nodeEntry NodeEntry, p2pUrl string, tmUrl string, nodekeyAddress string, name bool) {
	if name {
		fmt.Println("- Node_UserIdent: ", nodeEntry.NodeUserIdent)
	}
	// need the tm URL that is addressable from outside the cluster (ingress or nodeport).
	// need the enodeURL of the node, that is addressable from outside the cluster (ingress or nodeport).
	if k8sdir == "" {
		red.Println("Set --k8sdir flag or QUBE_K8S_DIR env in order to display enodeUrl")
	} else {
		enodeUrl := getEnodeUrl(nodeEntry.NodeUserIdent, k8sdir)
		// replace the internal dns addressable p2p @quorum-node1:30303? with an external p2p URL (nodeport)
		enodeUrl = strings.ReplaceAll(enodeUrl, nodeEntry.NodeUserIdent+":"+DefaultP2PPort, p2pUrl)
		fmt.Println("  Enode_Url:", enodeUrl)
	}
	fmt.Println("  Tm_Url: ", tmUrl)
	// if IBFT need the Node_Acct_Addr
	if nodekeyAddress != "" {
		fmt.Println("  Node_Acct_Addr:", nodekeyAddress)
	}
	// Acct_PubKey??
}

func displayExternalNode(extNode ExternalNodeEntry, isName, isEnodeUrl, isTmUrl, isNodekeyAddress bool) {
	if isName {
		fmt.Println("- Node_UserIdent: ", extNode.NodeUserIdent)
	}
	if isTmUrl {
		fmt.Println("  Tm_Url: ", extNode.TmUrl)
	}
	if isEnodeUrl {
		fmt.Println("  Enode_Url:", extNode.EnodeUrl)
	}
	// if IBFT need the Node_Acct_Addr
	if isNodekeyAddress && extNode.NodekeyAddress != "" {
		fmt.Println("  Node_Acct_Addr:", "\""+extNode.NodekeyAddress+"\"")
	}
	// Acct_PubKey??
}

func displayExternalNodeBare(extNode ExternalNodeEntry, isName, isEnodeUrl, isTmUrl, isNodekeyAddress bool) {
	if isName {
		fmt.Println(extNode.NodeUserIdent)
	}
	if isTmUrl {
		fmt.Println(extNode.TmUrl)
	}
	if isEnodeUrl {
		fmt.Println(extNode.EnodeUrl)
	}
	// if IBFT need the Node_Acct_Addr
	if isNodekeyAddress && extNode.NodekeyAddress != "" {
		fmt.Println("\"" + extNode.NodekeyAddress + "\"")
	}
	// Acct_PubKey??
}

// stop node should just remove the deployment, and not delete any resources or persistent data.
func stopNode(nodeName string) error {
	// TODO: should there be a separate delete and remove node? where remove only removes it from the cluster, but delete removes all traces?
	rmRunningDeployment := exec.Command("kubectl", "delete", "deployment", nodeName+"-deployment")
	fmt.Println(rmRunningDeployment)
	// TODO: run should return the error so we can handle it or ignore it.
	var out bytes.Buffer
	rmRunningDeployment.Stdout = &out
	err := rmRunningDeployment.Run()
	if err != nil { // log the error but don't throw any
		log.Info("deployment not found in k8s, ignoring.")
	}
	return err
}

// TODO: handle errors, etc.
func rmPersistentData(nodeName string) error {
	// remove the persistent data.
	rmPVC := exec.Command("kubectl", "delete", "pvc", nodeName+"-pvc")
	fmt.Println(rmPVC)
	var out bytes.Buffer
	rmPVC.Stdout = &out
	err := rmPVC.Run()
	if err != nil { // log the error but don't throw any
		log.Info("PVC / Persistent data not found in k8s, ignoring.")
	}
	return err
}

func rmService(nodeName string) error {
	// remove the persistent data.
	rmService := exec.Command("kubectl", "delete", "service", nodeName)
	fmt.Println(rmService)
	var out bytes.Buffer
	rmService.Stdout = &out
	err := rmService.Run()
	if err != nil { // log the error but don't throw any
		log.Info("service not found in k8s, ignoring.")
	}
	return err
}

func getTmPublicKey(nodeName string) string {
	//c1 := exec.Command("cat", qubeK8sDir+"/config/" + nodeKeyDir + "tm.pub")
	//kc get configMaps quorum-node3-tm-key-config -o yaml | grep "tm.pub:"
	c1 := exec.Command("kubectl", "get", "configMap", nodeName+"-tm-key-config", "-o", "yaml")
	c2 := exec.Command("grep", "tm.pub:")

	r, w := io.Pipe()
	c1.Stdout = w
	c2.Stdin = r

	var out bytes.Buffer
	c2.Stdout = &out
	c1.Start()
	c2.Start()
	c1.Wait()
	w.Close()
	c2.Wait()
	// output will look like:
	// tm.pub: dF+Y81qRKI3Noh6ldI+FnQmqmjRYvOqLCaooTi5txi4=
	tmPublicKey := strings.ReplaceAll(out.String(), "tm.pub:", "")
	tmPublicKey = strings.TrimSpace(tmPublicKey)
	return tmPublicKey
}
