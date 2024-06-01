# Redis clone
Based off of the challenge guide from [Codecrafters](https://app.codecrafters.io/courses/redis/introduction)

## Core functionalities
Redis is an in-memory data structure store often used as a database, cache, message broker and streaming engine. My clone's (limited) functionalities include:
1) Listening on and writing to TCP ports
2) Respond to multiple and concurrent client requests
3) Respond to client `PING`, `ECHO`, `SET`, and `GET` requests
4) Time expiry functionality for database key access 

## Additional functionalities
Redis also supports *leader-follower replication*, where replica/slave Redis instances are exact copies of master instances. The replica will attempt to copy the master's state exactly, and update the master with any changes made to the replica instance. My clone's (again, limited) replication functionalities include:
1) Custom host & port configuration
2) Respond to the `INFO` command: requests replication properties (e.g. 'master' vs 'slave' role) of the Redis instance
3) 3-step replication handshake: `PING`, `REPLCONF`, and `PSYNC`
4) Synchronize replica instances with an RDB file (contents of master database) 
5) Propagate changes to and from a master and its (multiple) replicas

## Demonstration
1) Download the source code!
2) Run `./spawn_redis_server.sh`
3) See the functionalities in action with the following bash commands (in a new terminal instance)!

### PING
Pings the server, receives a "PONG" response if successful

```echo '*1\r\n$4\r\nPING\r\n' | nc localhost 6379```

> PONG

### ECHO
The server sends back the argument as a bulk string

```echo '*2\r\n$4\r\nECHO\r\n$5\r\nhello\r\n' | nc localhost 6379```

> hello

### SET & GET
Set "foo" equal to "bar", then retrieves the value for "foo"

```echo '*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n' | nc localhost 6379```

> OK

```echo '*2\r\n$3\r\nGET\r\n$3\r\nfoo\r\n' | nc localhost 6379```

> bar

### Expiry
Set "foo" equal to "bar" with an expiration duration of 60,000 ms (i.e. 1 minute)

```echo '*5\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n$2\r\nPX\r\n$5\r\n60000\r\n' | nc localhost 6379```

No go grab a popcorn/feed the cats/file your taxes (or don't!), then:

```echo '*2\r\n$3\r\nGET\r\n$3\r\nfoo\r\n' | nc localhost 6379```

Depending on whether you've waited a full minute, you'll receive

> bar

if you've made it by the expiration time, or

> -1

if you're too late!

### Custom host & port
Start the Redis instance with your network address as an argument:

```./spawn_redis_server.sh --port <your address>```

and pipe your commands to `nc <your address>` instead!

### INFO replication
A server is a master by default:

```./spawn_redis_server.sh --port <master's address>```

A server is a replica to another master by supplying the `--replicaof <master's address>` argument

```./spawn_redis_server.sh --port <slave's address> --replicaof <master's address>```

Requesting `INFO replication` from a master server will yield its role as master (along with other fields):

```echo '*2\r\n$4\r\nINFo\r\n$11\r\nreplication\r\n' | nc <address>```

> role:master

But a replica server will respond with:

> role:slave

### Command propagation

A master will always try to keep its replicas coherent (i.e. in the same state as itself)

Assuming master listening on `<master's address>` and any replica on `<slave's address>`:

```echo '*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n' | nc <master's address>```

> OK

The command will be propagated to any and all replicas, and we can verify:

```echo '*2\r\n$3\r\nGET\r\n$3\r\nfoo\r\n' | nc <slave's address>```

> bar
