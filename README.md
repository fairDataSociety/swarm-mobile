# swarm-mobile

SwarmMobile is a bee client built with [fyne](https://fyne.io/) using [bee-lite](https://github.com/onepeerlabs/bee-lite). It can run on multiple platforms supported by fyne.

By default, it will run in testnet (goerli), It needs few modification to run on mainnet. 

## Build guide
To Build from source you will need fyne
```
go install fyne.io/fyne/v2/cmd/fyne@latest
```
### darwin
```
fyne package -os darwin
```
### linux 
```
fyne package -os linux
```
### windows
```
fyne package -os windows
```
### android & ios
```
fyne package -os android -appID com.plur.beemobile
fyne package -os ios -appID com.plur.beemobile
```

## TODO
- [ ] release for testnet and mainnet
- [ ] host binaries
- [ ] code review