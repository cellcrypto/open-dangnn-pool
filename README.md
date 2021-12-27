## Open Source Ethereum Mining Pool

![Miner's stats page](https://user-images.githubusercontent.com/7374093/31591180-43c72364-b236-11e7-8d47-726cd66b876a.png)

[![Join the chat at https://gitter.im/cellcrypto/open-ethereum-pool](https://badges.gitter.im/Join%20Chat.svg)](https://gitter.im/cellcrypto/open-ethereum-pool?utm_source=badge&utm_medium=badge&utm_campaign=pr-badge&utm_content=badge) [![Build Status](https://travis-ci.org/sammy007/open-ethereum-pool.svg?branch=develop)](https://travis-ci.org/sammy007/open-ethereum-pool) [![Go Report Card](https://goreportcard.com/badge/github.com/cellcrypto/open-ethereum-pool)](https://goreportcard.com/report/github.com/cellcrypto/open-ethereum-pool)

### Features

**This pool is being further developed to provide an easy to use pool for Ethereum miners. This software is functional however an optimised release of the pool is expected soon. Testing and bug submissions are welcome!**

* Support for HTTP and Stratum mining
* Detailed block stats with luck percentage and full reward
* Failover geth instances: geth high availability built in
* Modern beautiful Ember.js frontend
* Separate stats for workers: can highlight timed-out workers so miners can perform maintenance of rigs
* JSON-API for stats
* PPLNS block reward
* Changed important data from Redis to MariaDB
* Input log to db (log pool operation)
* Add reported hash rate
* Minor chart added
 - Hash amount, average hash amount, reported hash, share (per chart interval)
* Added hook handling when application is closed
* Added Miner's Reward Block tab
* Lock processing and thread processing during payment

#### Proxies

* [Ether-Proxy](https://github.com/cellcrypto/ether-proxy) HTTP proxy with web interface
* [Stratum Proxy](https://github.com/Atrides/eth-proxy) for Ethereum

### Building on Linux

Dependencies:

  * go >= 1.10
  * geth or parity
  * redis-server >= 2.8.0 (tested 6.2.5)
  * nodejs >= 4 LTS
  * nginx

**I highly recommend to use Ubuntu 16.04 LTS.**

First install  [go-ethereum](https://github.com/ethereum/go-ethereum/wiki/Installation-Instructions-for-Ubuntu).

Clone & compile:

    git config --global http.https://gopkg.in.followRedirects true
    git clone https://github.com/cellcrypto/open-ethereum-pool.git
    cd open-ethereum-pool
    make

Install redis-server.

### Running Pool

    ./build/bin/open-ethereum-pool config.json

You can use Ubuntu upstart - check for sample config in <code>upstart.conf</code>.

### Building Frontend

Install nodejs. I suggest using LTS version >= 4.x from https://github.com/nodesource/distributions or from your Linux distribution or simply install nodejs on Ubuntu Xenial 16.04.

The frontend is a single-page Ember.js application that polls the pool API to render miner stats.

    cd www

Change <code>ApiUrl: '//example.net/'</code> in <code>www/config/environment.js</code> to match your domain name. Also don't forget to adjust other options.

    npm install -g ember-cli@2.9.1
    npm install -g bower
    npm install
    bower install
    ./build.sh

Configure nginx to serve API on <code>/api</code> subdirectory.
Configure nginx to serve <code>www/dist</code> as static website.

#### Serving API using nginx

Create an upstream for API:

    upstream api {
        server 127.0.0.1:8080;
    }

and add this setting after <code>location /</code>:

    location /api {
        proxy_pass http://api;
    }

#### Customization

You can customize the layout using built-in web server with live reload:

    ember server --port 8082 --environment development

**Don't use built-in web server in production**.

Check out <code>www/app/templates</code> directory and edit these templates
in order to customise the frontend.

### Configuration

Configuration is actually simple, just read it twice and think twice before changing defaults.

**Don't copy config directly from this manual. Use the example config from the package,
otherwise you will get errors on start because of JSON comments.**

Look at the config.example.json file

If you are distributing your pool deployment to several servers or processes,
create several configs and disable unneeded modules on each server. (Advanced users)

I recommend this deployment strategy:

* Mining instance - 1x (it depends, you can run one node for EU, one for US, one for Asia)
* Unlocker and payouts instance - 1x each (strict!)
* API instance - 1x

### Notes

* Unlocking and payouts are sequential, 1st tx go, 2nd waiting for 1st to confirm and so on. You can disable that in code. Carefully read `docs/PAYOUTS.md`.
* Also, keep in mind that **unlocking and payouts will halt in case of backend or node RPC errors**. In that case check everything and restart.
* You must restart module if you see errors with the word *suspended*.
* Don't run payouts and unlocker modules as part of mining node. Create separate configs for both, launch independently and make sure you have a single instance of each module running.
* If `poolFeeAddress` is not specified all pool profit will remain on coinbase address. If it specified, make sure to periodically send some dust back required for payments.


### Credits

Made by sammy007. Licensed under GPLv3.
Modified by cellbit Team.

#### Contributors

[Alex Leverington](https://github.com/subtly)

### Donations

ETH/ETC: 0xb05146ed865f0ab592dd763bd84a2191700f3dfb

![](https://cdn.pbrd.co/images/GP5tI1D.png)

Highly appreciated.
