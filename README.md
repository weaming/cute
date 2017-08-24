# Cute

Page click statistic. Using redis as data backend.

## Usage

    go get -u github.com/weaming/cute
    cute -h

Visit `http://127.0.0.1:8080/click?host=<host>&uri=<uri>` to add record.

Visit http://127.0.0.1:8080 to view global statistics.

Visit [`http://127.0.0.1:8080/host/<host>`](http://127.0.0.1:8080/host/127.0.0.1:8080) to view single site's statistics.

Visit [`http://127.0.0.1:8080/ip/<ip>`](http://127.0.0.1:8080/ip/127.0.0.1) to query IP information.

## Go dependencies

Using [dep](https://github.com/golang/dep)

    dep init
    dep ensure