# Port forward the access port for the central server to :6441 which is open
kubectl config use-context kind-central
nohup kubectl port-forward --address 0.0.0.0 svc/pacman-central 6441:80 > central-server-port.log 2>&1 &

# Port forward caddy ports for chunk servers to ports in the range 36000-37000
# 364** represents the HTTPS port
# 368** represents the HTTP port
# Routing to these ports is managed by the bare metal server's Caddyconfig, and should be accessed via URLs - chunk1.clairegregg.com, eg
kubectl config use-context kind-chunk1
nohup kubectl port-forward --address 0.0.0.0 svc/caddy 36801:80 > caddy-1-http-port.log 2>&1 &
nohup kubectl port-forward --address 0.0.0.0 svc/caddy 36401:443 > caddy-1-https-port.log 2>&1 &

kubectl config use-context kind-chunk2
nohup kubectl port-forward --address 0.0.0.0 svc/caddy 36802:80 > caddy-2-http-port.log 2>&1 &
nohup kubectl port-forward --address 0.0.0.0 svc/caddy 36402:443 > caddy-2-https-port.log 2>&1 &

kubectl config use-context kind-chunk3
nohup kubectl port-forward --address 0.0.0.0 svc/caddy 36803:80 > caddy-3-http-port.log 2>&1 &
nohup kubectl port-forward --address 0.0.0.0 svc/caddy 36403:443 > caddy-3-https-port.log 2>&1 &