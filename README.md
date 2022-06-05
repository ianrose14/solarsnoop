# solarsnoop
Monitor your Enphase-managed solar panels and be notified when your system is overproducing



## VM setup:

* sudo apt-get install podman
* sudo iptables -A PREROUTING -t nat -p tcp --dport 80 -j REDIRECT --to-port 8080
* sudo iptables -A PREROUTING -t nat -p tcp --dport 443 -j REDIRECT --to-port 8443
* sudo create `/etc/containers/containers.conf` with contents:
```
[engine]
cgroup_manager = "cgroupfs"
```


# delete this stuff later:

for debugging:
* sudo python3 -m http.server 80


TODOs:
* go build cache for docker
* 
