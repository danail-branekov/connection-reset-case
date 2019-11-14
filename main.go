package main

import (
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/garden/client"
	"code.cloudfoundry.org/garden/client/connection"
	"fmt"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Println("two arguments expected")
	}

	containerName := os.Args[1]
	pidLimit, err := strconv.Atoi(os.Args[2])
	if err != nil {
		fmt.Printf("failed to convert %q to int: %v\n", os.Args[2], err)
		return
	}

	gardenClient := createGardenClient()
	container, err := createContainer(gardenClient, containerName, pidLimit)
	if err != nil {
		fmt.Printf("failed to create container %q to int: %v\n", containerName, err)
		return
	}
	defer func() {
		deleteContainer(gardenClient, container)
	}()

	processSpec := garden.ProcessSpec{
		Path: "echo",
		Args: []string{"hi"},
	}

	processExitCode, err := runProcess(container, processSpec)
	if err != nil {
		fmt.Printf("failed to run the container process: %s\n", err.Error())
		return
	}
	if processExitCode != 0 {
		fmt.Printf("process exited with non-zero exit code %d", processExitCode)
		return
	}

	fmt.Println("container process finished successfully")
}

func runProcess(container garden.Container, processSpec garden.ProcessSpec) (int, error) {
	proc, err := container.Run(processSpec,garden.ProcessIO{})
	if err != nil {
		return -1, err
	}
	return proc.Wait()
}


func deleteContainer(gardenClient garden.Client, container garden.Container) {
	if err := gardenClient.Destroy(container.Handle()); err != nil {
		fmt.Println("Failed to delete container %s: %v", container.Handle(), err)
	}
}

func createContainer(gardenClient garden.Client, containerName string, pidLimit int) (garden.Container, error) {
	containerLimits := garden.Limits{
		Pid: garden.PidLimits{
			Max: uint64(pidLimit),
		},
	}
	return gardenClient.Create(garden.ContainerSpec{
		Handle: containerName,
		Limits: containerLimits,
	})
}

func createGardenClient() garden.Client {
	gardenHost := os.Getenv("GARDEN_ADDRESS")
	if gardenHost == "" {
		gardenHost = "10.244.0.2"
	}
	gardenPort := os.Getenv("GARDEN_PORT")
	if gardenPort == "" {
		gardenPort = "7777"
	}

	gardenConnection := connection.New("tcp", fmt.Sprintf("%s:%s", gardenHost, gardenPort))
	return client.New(gardenConnection)
}
