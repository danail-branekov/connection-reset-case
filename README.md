# The Garden Team and the Strange Case of a Connection Being Reset

We all know that the [night is dark and full of flakes](https://medium.com/@teddyking/the-night-is-dark-and-full-of-flakes-29c529404c3c). There are different kind of flakes though: some of them are like a refreshing walk in the park, some make you shiver like a sudden gust of wind on a cold winter day, others are as scary as the nightmare that wakes you up in the middle of the night in cold sweat over and over again.

Today I am going to tell you a story about a flake that felt like an emotional roller coaster that used to elevate us to the highest hopes and then throw us down into total despair and rejection, but also a story about unforeseen help from a friend. ~~My laziness~~The bitter memory has been making us put writing this article off for more than an year so that we almost lost context on what happened... but finally here it is.   

## Who are we?
We are Garden, the team that builds the Cloud Foundry container engine. Every time you want **MOAR** resources for your Cloud Foundry application, the container engine is there to give you **MOAR** containers so that your application gets even more **EPIC**.

Containers, especially the Linux ones, are quite exciting themselves. If you are new to containers I would highly recommend reading the [excellent tutorial](https://github.com/georgethebeatle/diycontainers) on DIY containers by my colleague Georgi. In order the article to make sense, you should know that our container engine uses [runc](https://github.com/opencontainers/runc) under the hood to go through most of them Linux hoops.


The container engine runs on special Cloud Foundry VMs (called _Diego cells_) as a daemon that exposes API to manage containers, such as `create a container`, `delete a container`, `list containers`, `run a process in a container`. Cloud Foundry users usually do not have access to that API, however when they, for example, scale an application up that would eventually result into a `create a container` request to the container engine. The curious reader could have a look at the [Cloud Foundry architecture docs](https://docs.cloudfoundry.org/concepts/architecture/garden.html) for all the nitty-gritty.

## Once upon a time...
... there was a continuous integration (CI) environment which ran all Garden automated tests. As Garden is all about containers, most of those tests create at least one container. Even though containers themselves are exciting, they are quite useless if they do not contain anything, that's why tests usually proceed with executing a process in that container. Unfortunately, sometimes process execution would fail with an error:
```
runc exec: exit status 1: exec failed: container_linux.go:348: starting container process caused "read init-p: connection reset by peer"
```
As it was pretty hard to reproduce the failure on our local test environment we wanted to believe that this is _one of those things that do not happen to good people on production_.  Alas, our CI records showed that the failure happened several times per week so we had to bite the bullet and debug the crap out of it.

## 20180806: It can't be a bug in Garden... right?
At the time those mysterious failures started to show up Garden process execution flow has not been changed since ages. Therefore the most obvious explanation was that we had bumped a dependency of ours thus picking up some else's bugs. And the most obvious culprit was [Golang](https://golang.org/) (one of the languages we use to talk to computers) itself. There was some tribal knowledge that we had not experienced the flake with Golang 1.9.4 so our first shot was to create a test environment running our tests against Golang 1.9.4 for a while just to make sure that the flake is not available there.
* Hope level: 1
* Desperation level: 0

## 20180809: No, it is not Golang
Three days later the flake happened on our Golang 1.9.4 test environment.
* Hope level: 0
* Desperation level: 1

## 20180820: Okay, lets try to understand what it going on
As mentioned above, we had not changed the process execution flow for long so it was time to roll up our sleeves and meet the monsters who dwell in runc. It turned out that the string `init-p` is actually the [name of the parent pipe](https://github.com/opencontainers/runc/blob/46def4cc4cb7bae86d8c80cedd43e96708218f0a/libcontainer/utils/utils_unix.go#L67) (hence `-p`) of pipe pairs that are used by parent processes to talk to their children and vice versa. The error message suggests that there is a parent process getting messages from its child over a _parent_ pipe and occasionally a `connection reset` error occurs while doing so. Indeed, when `runc` executes a new process in the container, it sets up a pipes pair and uses it to talk to the child process.

Wait a minute, `runc` starting a new process in the container does make lots of sense, but why would it want to talk to it? Why wouldn't `runc` just start a process and just let it be itself and do its processy things? Well, if you have already read the [DIY containers tutorial](https://github.com/georgethebeatle/diycontainers), you already know that processes in a container run in their own namespace (or set of [Linux namespaces](http://man7.org/linux/man-pages/man7/namespaces.7.html), if you want to be technically precise) that has been created when the container is initially created. Therefore when executing a process in the container we want the new process to run in exactly that namespace.

Unfortunately, running processes in a given namespace in Linux is not _that_ trivial:
* First, `runc` needs to [clone](http://man7.org/linux/man-pages/man2/clone.2.html) into a new process which would eventually execute the binary of the process we want to run in the container
* Then, the cloned process needs to [unshare](http://man7.org/linux/man-pages/man2/unshare.2.html) the inherited namespace and enter the container namespace
* Finally, the cloned process executes the binary of the requested container process into the container namespace
> As a matter of fact, things are even more complex, Alexa Sarai explains all the additional complexity in details [here](https://github.com/opencontainers/runc/blob/46def4cc4cb7bae86d8c80cedd43e96708218f0a/libcontainer/nsenter/nsexec.c#L642-L687). Don't worry if you have no idea what he is talking about after reading the explanation for the first time, it took me months to wrap my head around it.

`Runc` needs to keep track of all that machinery so that it eventually returns the PID of the process executed in the container to Garden. All the necessary communication and synchronization is carried out over the aforementioned pipe pairs through special JSON messages.

Back to our flake, it seems that for some reason the new container process terminates unexpectedly and therefore the parent process (i.e. `runc`) is getting a `connection reset` while trying to read messages from its child over the pipes pair.
* Hope level: 0
* Desperation level: 5 (due to all that process-namespace madness)

## 20180821 (morning): Maybe someone already fixed that for us? 🤞
Why wasting time in trying to fix something if someone already did that for us? Let's bump `runc` to latest, hopefully that should do it
* Hope level: 1
* Desperation level: 5

## 20180821 (afternoon): No, it is not fixed
Our CI reproduced the flake almost immediately :(
* Hope level: 0
* Desperation level: 6

## 20180822: Logs or it did not happen!
We find `runc` not really chatty, often puzzling us what a problem could be. We decided that we need to update our reproduction CI environment with a custom `runc` build that produces extra log lines on interesting places. That should give us all the details we need to figure things out!
* Hope level: 1
* Desperation level: 6

## ~20180917: Fast-forward about a month
Additional logs did not help us find an explanation why the child process dies
* Hope level: 0
* Desperation level: 10

## 20180918: The corrupted message
Remember the pipes pairs and the JSON messages parent/child processes talk to each other? If you ever wrote some code utilising JSON communication you know that JSON marshalling/unmarshalling is an inevitable part of the game.

We came across an [old moby issue](https://github.com/moby/moby/issues/14203#issuecomment-174177790); according to the people involved, the writing end of the pipe encodes a new line (`\n`) as last character in the message. But that last character doesn't always get into the reader's decoding buffer. Therefore it may happen that the reader has reached the last useful character (`}`) of the JSON message and thinks that the message is over. However, it finds an obsolete new line character remaining in the pipe and freaks out. Maybe we could inspect our inter-process communication very-VERY-**VERY** closely and find where such a new line could come from.
* Hope level: 1
* Desperation level: 10

## 20180927: Let's get rid of that new line
The only place `runc` would add an additional new line to the parent-child JSON message is [here](https://github.com/opencontainers/runc/blob/aaf210ac5dcf1b26871558cf6532d71772d4cd70/libcontainer/nsenter/nsexec.c#L769). Let's update our custom `runc` build in our CI with a version that has this line removed
* Hope level: 2
* Desperation level: 10

## 20181003: You guessed it - still not fixed
Well, it happened again...
* Hope level: 0
* Desperation level: 12

## 20181017: (Get By) With a Little Help from My Friends
Several days later I have been working on an [unrelated bug](https://github.com/cloudfoundry/garden-runc-release/issues/98) reported by our Diego friends. In short, their tests are fine, however they noticed a go routine leak caused by Garden after running their test suite.

A few hours later we managed to isolate [a test](https://github.com/cloudfoundry/vizzini/blob/7e89abaad7c7a25c122b059f7dc7ac5f5f00df8d/max_pids_test.go#L46-L55) that caused such a goroutine leak. The test starts a container with PID limit of `1`, then runs a process and expects that to fail. I am not going to discuss container limits here, but a container with PID limit of `1` means that there cannot be more than one process running in the container. As a container always has an `init` process (started once the container is created), then its limit is already exhausted, hence failing to run the process makes sense.
> Setting the maximum number of processes in a container is implemented via the [PID cgroup](https://www.kernel.org/doc/Documentation/cgroup-v1/pids.txt). Each container is created in its own cgroup, setting the PID limit of a container actually writes the limit number into `pids.max` of the container PID cgroup) 

Okay, let's bash a program (full source code [here](./main.go)) that does that, we can check for go routine leaks after it is done:
```go
func main() {
    // Boring initializations
	container, err := createContainer(gardenClient, containerName, pidLimit)
    // Boring error handling

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
    
    // More boring stuff
}
```
The code above would create a container and run `echo hi` into that container, easy peasy! Let's run it!
```console
# ./bin/conn-res mycontainer 1
failed to run the container process: runc exec: exit status 1: exec failed: container_linux.go:346: starting container process caused "read init-p: connection reset by peer"
```
WAIT! WHAAAAAAT!? HOW? WHY? Did we just manage to reproduce our flake!?
* Hope level: 100
* Desperation level: 12


## 20181018: The investigation
Let's see whether our reproduction is reliable:
```console
# ./bin/conn-res mycontainer 1
failed to run the container process: runc exec: exit status 1: exec failed: container_linux.go:346: starting container process caused "read init-p: connection reset by peer"
```

Hmmm... let's try with 10
```console
# ./bin/conn-res mycontainer 10
container process finished successfully
```

So what is the pid limit when the `connection reset` error start to appear? Let's play with the pid limit values:
```console
# ./bin/conn-res mycontainer 9
container process finished successfully

# ./bin/conn-res mycontainer 8
container process finished successfully

# ./bin/conn-res mycontainer 7
failed to run the container process: runc exec: exit status 1: exec failed: container_linux.go:346: starting container process caused "read init-p: connection reset by peer"

# ./bin/conn-res mycontainer 7
failed to run the container process: runc exec: exit status 1: exec failed: container_linux.go:346: starting container process caused "read init-p: connection reset by peer"

# ./bin/conn-res mycontainer 7
failed to run the container process: runc exec: exit status 1: exec failed: container_linux.go:346: starting container process caused "read init-p: connection reset by peer"

# ./bin/conn-res mycontainer 7
container process finished successfully

# ./bin/conn-res mycontainer 8
container process finished successfully

# ./bin/conn-res mycontainer 8
container process finished successfully

# ./bin/conn-res mycontainer 8
container process finished successfully

# ./bin/conn-res mycontainer 8
failed to run the container process: runc exec: exit status 1: exec failed: container_linux.go:346: starting container process caused "read init-p: connection reset by peer"
```
Interesting, it seems that pid limit of `7` or `8` makes process execution flaky. But why? We are running a single process i.e. we are running total of 2 processes (the container `init` and `echo`), pid limit of 7 or 8 should be pretty much enough, right?
After some digging in the runc code and various experimental programs in C we found out that:
* The pid cgroup limit is applied to threads started by a process (via e.g. [`fork`](http://man7.org/linux/man-pages/man2/fork.2.html), [`clone`](http://man7.org/linux/man-pages/man2/clone.2.html), [`pthread_create`](http://man7.org/linux/man-pages/man3/pthread_create.3.html)), not only to processes themselves
* There is a race in `runc`
    * the new process is started without being waited for ([here](https://github.com/opencontainers/runc/blob/c2ab1e656e7af78bde396511be003b9903f004a3/libcontainer/process_linux.go#L76))
    * as the process runs, `runc` would put the processes into the limited container cgroup ([here](https://github.com/opencontainers/runc/blob/c2ab1e656e7af78bde396511be003b9903f004a3/libcontainer/process_linux.go#L90-L92))
The conditions above create a race where the process can join the restricted cgroup too early while the Golang runtime is initializing and creating its internal threads

In order to prove that we added a sleep of 100ms before the process is joined to the cgroup and this significantly reduced the failure rate when the PID limit is `7`. Removing the code that joins the cgroup "fixed" it entirely.
* Hope level: 100
* Desperation level: 9
* Satisfaction level: 9999999999999999

## The aftermath
Once we got to the bottom we had a closer look at the flake history in our CI. And guess what - all the tests that flaked set the container pid limit to `10` or `20`. We should have spotted the pattern earlier, doh!

It is all now clear - you should never set your container limits to ridiculously low values. In the end it turned out that this flake is _one of those things that do not happen to good people on production_ indeed - good people never set their pid limits to `1`

As usual, we make the world a better place, so we raised this ["documentation issue"](https://github.com/opencontainers/runc/issues/1914) with `runc` to share the knowledge with the community.

As per the flake itself - we just had to [increase the pid limit to 50](https://github.com/cloudfoundry/garden-integration-tests/commit/e2d81217e321f9321b8d5a5d73d2050a4faedca7) in our tests and kiss the flake goodbye!

## Closing Credits
This write-up would have never been written without the help and support of all gardeners, at time of this thriller
* Claudia Beresford
* Julia Nedialkova
* Georgi Sabev
* Tom Godkin
* Giuseppe Capizzi
* Oliver Stenbom
* Danail Branekov

Product Manager: Julz Friedman

A huge **THANK YOU!** goes to our Diego friends who pointed us to the flake root cause by accident.
