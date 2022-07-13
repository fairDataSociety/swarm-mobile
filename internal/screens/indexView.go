package screens

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/big"
	"strconv"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ethereum/go-ethereum/ethclient"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/ethersphere/bee/pkg/logging"
	"github.com/ethersphere/bee/pkg/swarm"
	"github.com/onepeerlabs/bee-lite"
	"github.com/sirupsen/logrus"
)

const (
	TestnetChainID = 5 //testnet
	MainnetChainID = 100
)

var (
	MainnetBootnodes = []string{
		"/ip4/142.132.198.19/tcp/32225/p2p/16Uiu2HAmRPHcJupNaUivskPocWKwLmpV2ApXkawesPAaJXv67y4b",
		"/ip4/142.132.208.116/tcp/32400/p2p/16Uiu2HAmJeGwCSzWo3mkRTsRd3fzxf7Fk1hkJ9dDkXmkxpjoNPmH",
		"/ip4/142.132.198.150/tcp/32259/p2p/16Uiu2HAmCvdVXazhxwCJbnqBqkB7pot7o4RbggTZVN7VrUYinxBx",
	}

	TestnetBootnodes = []string{
		"/ip4/65.108.101.3/tcp/32000/p2p/16Uiu2HAmBRwS1SF79jEz8dDNfdqQvUXophexSbE5PDxLCxVEuB38",
	}
)

type logger struct{}

func (*logger) Write(p []byte) (int, error) {
	log.Println(string(p))
	return len(p), nil
}

type index struct {
	fyne.Window

	app          fyne.App
	view         *fyne.Container
	content      *fyne.Container
	progress     dialog.Dialog
	activeWindow string
	mtx          sync.Mutex
	b            *bee.Bee
}

type uploadedItem struct {
	Name      string
	Reference string
	Size      int64
	Timestamp int64
	Mimetype  string
}

func Make(a fyne.App, w fyne.Window) fyne.CanvasObject {
	i := &index{
		Window: w,
		app:    a,
	}
	path := a.Storage().RootURI().Path()
	// check if password is set
	password := i.app.Preferences().String("password")
	if password != "" {
		swapEndpoint := i.app.Preferences().String("SwapEndpoint")
		if swapEndpoint == "" {
			i.view = container.NewBorder(nil, nil, nil, nil, i.showRPCView(path, password))
			return i.view
		}
		go i.start(path, password, swapEndpoint)
		blankView := container.NewBorder(widget.NewLabel(""), widget.NewLabel(""), widget.NewLabel(""), widget.NewLabel(""), widget.NewLabel("Please wait..."))
		i.view = container.NewBorder(nil, nil, nil, nil, blankView)
		return i.view
	}
	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("Password")
	form := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "Password", Widget: passwordEntry, HintText: "Password"},
		},
		OnSubmit: func() {
			if passwordEntry.Text == "" {
				i.showError(fmt.Errorf("passwordEntry cannot be blank"))
				return
			}
			swapEndpoint := i.app.Preferences().String("SwapEndpoint")
			if swapEndpoint == "" {
				i.view.Objects[0] = container.NewBorder(nil, nil, nil, nil, i.showRPCView(path, passwordEntry.Text))
				return
			}
			i.start(path, passwordEntry.Text, swapEndpoint)
		},
	}
	i.view = container.NewBorder(nil, nil, nil, nil, form)
	return i.view
}

func (i *index) showRPCView(path, password string) fyne.CanvasObject {
	rpcEntry := widget.NewEntry()
	rpcEntry.SetPlaceHolder("RPC")
	form := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "RPC Endpoint", Widget: rpcEntry, HintText: "RPC Endpoint"},
		},
		OnSubmit: func() {
			if rpcEntry.Text == "" {
				i.showError(fmt.Errorf("rpc endpoint cannot be blank"))
				return
			}
			i.start(path, password, rpcEntry.Text)
		},
	}
	return form
}

func (i *index) start(path, password, rpc string) {
	if password == "" {
		i.showError(fmt.Errorf("password cannot be blank"))
		return
	}
	i.showProgressWithMessage("Starting Bee")
	// this runs on testnet
	mainnet := false
	err := i.initSwarm(path, path, "welcome from bee-lite", password, rpc, mainnet, logrus.DebugLevel)
	i.hideProgress()
	if err != nil {
		addr, addrErr := bee.OverlayAddr(path, password)
		if addrErr != nil {
			i.showError(addrErr)
			return
		}
		i.showErrorWithAddr(addr, err)
		return
	}

	i.app.Preferences().SetString("SwapEndpoint", rpc)
	err = i.initView()
	if err != nil {
		i.showError(err)
		return
	}
}

func (i *index) initSwarm(keystore, dataDir, welcomeMessage, password, swapEndpoint string, mainnet bool, logLevel logrus.Level) error {
	// check rpc endpoint & chainID
	eth, err := ethclient.Dial(swapEndpoint)
	if err != nil {
		return err
	}

	// check connection
	chainID, err := eth.ChainID(context.Background())
	if err != nil {
		return err
	}
	o := &bee.Options{
		FullNodeMode:             true,
		Keystore:                 keystore,
		DataDir:                  dataDir,
		Addr:                     ":1836",
		WelcomeMessage:           welcomeMessage,
		Bootnodes:                TestnetBootnodes,
		Logger:                   logging.New(&logger{}, logLevel),
		SwapEndpoint:             swapEndpoint,
		SwapInitialDeposit:       "10000000000000000",
		SwapEnable:               true,
		WarmupTime:               0,
		ChainID:                  TestnetChainID,
		ChequebookEnable:         true,
		ChainEnable:              true,
		BlockTime:                uint64(5),
		PaymentThreshold:         "100000000",
		UsePostageSnapshot:       false,
		Mainnet:                  true,
		NetworkID:                10,
		DBOpenFilesLimit:         50,
		DBWriteBufferSize:        32 * 1024 * 1024,
		DBDisableSeeksCompaction: false,
		DBBlockCacheCapacity:     32 * 1024 * 1024,
		RetrievalCaching:         true,
	}
	if mainnet {
		o.ChainID = MainnetChainID
		o.NetworkID = 1
		o.Mainnet = mainnet
		o.Bootnodes = MainnetBootnodes
	}
	if chainID.Int64() != o.ChainID {
		return fmt.Errorf("rpc endpoint point to wrong chain")
	}

	b, err := bee.Start(o, password)
	if err != nil {
		return err
	}
	i.app.Preferences().SetString("password", password)
	i.b = b
	return err
}

func (i *index) initView() error {
	peerCountBinding := binding.NewString()
	peerCountBinding.Set("0")
	connectionBinding := binding.NewString()
	connectionBinding.Set("You are connected")
	populationBinding := binding.NewString()
	populationBinding.Set("0")

	go func() {
		// auto reload
		for {
			select {
			case <-time.After(time.Second * 5):
				if i.activeWindow == "home" && i.b != nil {
					peerCountBinding.Set(fmt.Sprintf("%d", i.b.Topology().Connected))
					populationBinding.Set(fmt.Sprintf("%d", i.b.Topology().Population))
				}
			}
		}
	}()
	toolbar := widget.NewToolbar(
		widget.NewToolbarAction(theme.HomeIcon(), func() {
			if i.activeWindow != "home" {
				i.mtx.Lock()
				i.activeWindow = "home"
				i.mtx.Unlock()

				peerCountBinding.Set(fmt.Sprintf("%d", i.b.Topology().Connected))
				populationBinding.Set(fmt.Sprintf("%d", i.b.Topology().Population))
				connection := container.NewHBox(widget.NewLabel("Status : "), widget.NewLabelWithData(connectionBinding))
				peers := container.NewHBox(widget.NewLabel("Peers : "), widget.NewLabelWithData(peerCountBinding))
				population := container.NewHBox(widget.NewLabel("Population : "), widget.NewLabelWithData(populationBinding))
				content := container.NewGridWithColumns(1, container.NewVBox(connection, peers, population))
				i.content.Objects[0] = container.NewBorder(nil, nil, nil, nil, content)
			}
		}),
		widget.NewToolbarSpacer(),
		widget.NewToolbarAction(theme.FileIcon(), func() {
			if i.activeWindow != "file" {
				i.mtx.Lock()
				i.activeWindow = "file"
				i.mtx.Unlock()

				uploadHeader := widget.NewLabel("Uploaded content")
				uploadedContent := container.NewVBox(uploadHeader)
				uploadedContentWrapper := container.NewScroll(uploadedContent)
				uploadedSrt := i.app.Preferences().String("uploads")
				uploads := []uploadedItem{}
				if uploadedSrt != "" {
					err := json.Unmarshal([]byte(uploadedSrt), &uploads)
					if err != nil {
						i.showError(err)
					}
					for _, v := range uploads {
						ref := v.Reference
						name := v.Name
						label := widget.NewLabel(name)
						label.Wrapping = fyne.TextWrapWord
						item := container.NewBorder(label, nil, nil, widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
							i.Window.Clipboard().SetContent(ref)
						}))
						uploadedContent.Add(item)
					}
				}

				hash := widget.NewEntry()
				hash.SetPlaceHolder("Swarm Hash")
				dlForm := &widget.Form{
					Items: []*widget.FormItem{
						{Text: "Swarm Hash", Widget: hash, HintText: "Swarm Hash"},
					},
					OnSubmit: func() {
						dlAddr, err := swarm.ParseHexAddress(hash.Text)
						if err != nil {
							i.showError(err)
							return
						}
						go func() {
							i.progress = dialog.NewProgressInfinite("",
								fmt.Sprintf("Downloading %s", shortenHashOrAddress(hash.Text)), i)
							i.progress.Show()
							r, fileName, err := i.b.GetBzz(context.Background(), dlAddr)
							if err != nil {
								i.progress.Hide()
								i.showError(err)
								return
							}
							hash.Text = ""
							data, err := ioutil.ReadAll(r)
							if err != nil {
								i.progress.Hide()
								i.showError(err)
								return
							}
							i.progress.Hide()
							saveFile := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
								if err != nil {
									i.showError(err)
									return
								}
								if writer == nil {
									return
								}
								_, err = writer.Write(data)
								if err != nil {
									i.showError(err)
									return
								}
								writer.Close()
							}, i.Window)
							saveFile.SetFileName(fileName)
							saveFile.Show()
						}()

					},
				}
				filepath := ""
				mimetype := ""
				var pathBind = binding.BindString(&filepath)
				path := widget.NewEntry()
				path.Bind(pathBind)
				path.Disable()
				var file io.Reader
				openFile := widget.NewButton("File Open", func() {
					fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
						if err != nil {
							i.showError(err)
							return
						}
						if reader == nil {
							return
						}
						defer reader.Close()
						data, err := ioutil.ReadAll(reader)
						if err != nil {
							i.showError(err)
							return
						}

						mimetype = reader.URI().MimeType()
						pathBind.Set(reader.URI().Name())
						file = bytes.NewReader(data)
						data = nil
					}, i.Window)
					fd.Show()
				})
				upForm := &widget.Form{
					Items: []*widget.FormItem{
						{Text: "Add file", Widget: path, HintText: "Filepath"},
						{Text: "Choose File", Widget: openFile},
					},
				}
				upForm.OnSubmit = func() {
					go func() {
						defer func() {
							pathBind.Set("")
							file = nil
						}()
						if file == nil {
							i.showError(fmt.Errorf("please select a file"))
							return
						}
						batch := i.app.Preferences().String("batch")
						if batch == "" {
							i.showError(fmt.Errorf("please select a batch of stamp"))
							return
						}
						i.progress = dialog.NewProgressInfinite("",
							fmt.Sprintf("Uploading %s", path.Text), i)
						i.progress.Show()

						ref, err := i.b.AddFileBzz(context.Background(), batch, path.Text, mimetype, file)
						if err != nil {
							i.progress.Hide()
							i.showError(err)
							return
						}
						d := dialog.NewCustomConfirm("Upload successful", "Ok", "Cancel", i.refDialog(ref.String()), func(b bool) {}, i.Window)
						i.progress.Hide()
						d.Show()
						filename := path.Text
						uploads = append(uploads, uploadedItem{
							Name:      filename,
							Reference: ref.String(),
						})
						data, err := json.Marshal(uploads)
						if err != nil {
							i.progress.Hide()
							i.showError(err)
							return
						}
						i.app.Preferences().SetString("uploads", string(data))
						label := widget.NewLabel(filename)
						label.Wrapping = fyne.TextWrapWord
						refCopyButton := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
							i.Window.Clipboard().SetContent(ref.String())
						})
						item := container.NewBorder(label, nil, nil, refCopyButton)
						uploadedContent.Add(item)
						tabs := container.NewAppTabs(
							container.NewTabItemWithIcon("Upload", theme.UploadIcon(), container.NewBorder(upForm, nil, nil, nil, uploadedContentWrapper)),
							container.NewTabItemWithIcon("Download", theme.DownloadIcon(), container.NewVBox(dlForm)),
						)
						i.content.Objects[0] = container.NewBorder(nil, nil, nil, nil, container.NewBorder(nil, nil, nil, nil, tabs))
					}()
				}
				tabs := container.NewAppTabs(
					container.NewTabItemWithIcon("Upload", theme.UploadIcon(), container.NewBorder(upForm, nil, nil, nil, uploadedContentWrapper)),
					container.NewTabItemWithIcon("Download", theme.DownloadIcon(), container.NewVBox(dlForm)),
				)
				i.content.Objects[0] = container.NewBorder(nil, nil, nil, nil, container.NewBorder(nil, nil, nil, nil, tabs))
			}
		}),
		widget.NewToolbarSpacer(),
		widget.NewToolbarAction(theme.AccountIcon(), func() {
			if i.activeWindow != "accounts" {
				i.mtx.Lock()
				i.activeWindow = "accounts"
				i.mtx.Unlock()
				addrCopyButton := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
					i.Window.Clipboard().SetContent(i.b.Addr().String())
				})
				addr := container.NewHBox(
					widget.NewLabel("Addr"),
					widget.NewLabel(shortenHashOrAddress(i.b.Addr().String())),
					addrCopyButton,
				)

				// chequebook section
				chequebookHeader := widget.NewLabel("Chequebook")
				chequebookBalance, err := i.b.ChequebookBalance()
				if err != nil {
					dialog.ShowError(err, i.Window)
					return
				}
				withdrawButton := widget.NewButton("Withdraw", func() {})
				withdrawButton.OnTapped = func() {
					amount := big.NewInt(1000000000000000)
					if chequebookBalance.Cmp(amount) < 0 {
						dialog.ShowError(fmt.Errorf("not enough balance on chequebook"), i.Window)
						return
					}
					i.showProgressWithMessage(fmt.Sprintf("withdrawing %s from chequebook", amount.String()))
					tx, err := i.b.ChequebookWithdraw(amount)
					if err != nil {
						i.hideProgress()
						dialog.ShowError(err, i.Window)
						return
					}
					i.hideProgress()
					d := dialog.NewCustomConfirm("Withdrawal successful", "Ok", "Cancel", i.refDialog(tx.String()), func(b bool) {}, i.Window)
					d.Show()
				}
				chequebookContent := container.NewVBox(container.NewHBox(chequebookHeader, withdrawButton), widget.NewLabel(fmt.Sprintf("Balance: %s", chequebookBalance.String())))
				// stamps section
				buyButton := widget.NewButton("Buy", func() {})
				stampsHeader := container.NewHBox(widget.NewLabel("Postage stamps"), buyButton)
				stamps := i.b.GetAllBatches()

				selectedStamp := i.app.Preferences().String("selected_stamp")
				stampsList := []string{}
				for _, v := range stamps {
					stampsList = append(stampsList, shortenHashOrAddress(hex.EncodeToString(v.ID())))
				}
				radio := widget.NewRadioGroup(stampsList, func(s string) {
					if s == "" {
						return
					}
					for _, v := range stamps {
						stamp := hex.EncodeToString(v.ID())
						if s[0:6] == stamp[0:6] {
							i.app.Preferences().SetString("selected_stamp", s)
							i.app.Preferences().SetString("batch", stamp)
						}
					}
				})
				radio.SetSelected(selectedStamp)
				stampsContent := container.NewVBox(stampsHeader, radio)

				buyButton.OnTapped = func() {
					depthStr := "22"
					amountStr := "100000000"
					d := dialog.NewCustomConfirm("Buy Stamp", "Ok", "Cancel", i.buyBatchDialog(&depthStr, &amountStr), func(b bool) {
						if b {
							amount, ok := big.NewInt(0).SetString(amountStr, 10)
							if !ok {
								i.showError(fmt.Errorf("invalid amountStr"))
								return
							}
							depth, err := strconv.ParseUint(depthStr, 10, 8)
							if err != nil {
								i.showError(fmt.Errorf("invalid depthStr %s", err.Error()))
								return
							}
							i.showProgressWithMessage("buying stamp")
							id, err := i.b.BuyStamp(amount, depth, "", false)
							if err != nil {
								i.hideProgress()
								i.showError(err)
								return
							}
							i.hideProgress()
							stamp := hex.EncodeToString(id)
							stampCopyButton := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
								i.Window.Clipboard().SetContent(stamp)
							})
							stampsContent.Add(container.NewVBox(widget.NewLabel(shortenHashOrAddress(stamp)), stampCopyButton))
							content := container.NewGridWithColumns(1, container.NewVBox(addr, stampsContent))
							i.content.Objects[0] = container.NewBorder(nil, nil, nil, nil, content)
						}
					}, i.Window)
					d.Show()
				}
				content := container.NewGridWithColumns(1, container.NewVBox(addr, chequebookContent, stampsContent))
				i.content.Objects[0] = container.NewBorder(nil, nil, nil, nil, content)
			}
		}),
		widget.NewToolbarSpacer(),
		widget.NewToolbarAction(theme.SettingsIcon(), func() {
			if i.activeWindow != "settings" {
				i.mtx.Lock()
				i.activeWindow = "settings"
				i.mtx.Unlock()
				i.content.Objects[0] = container.NewBorder(nil, nil, nil, nil, widget.NewLabel("Settings"))
			}
		}),
	)
	i.activeWindow = "home"
	connection := container.NewHBox(widget.NewLabel("Status : "), widget.NewLabelWithData(connectionBinding))
	peers := container.NewHBox(widget.NewLabel("Peers : "), widget.NewLabelWithData(peerCountBinding))
	population := container.NewHBox(widget.NewLabel("Population : "), widget.NewLabelWithData(populationBinding))
	i.content = container.NewGridWithColumns(1, container.NewVBox(connection, peers, population))
	i.view.Objects[0] = container.NewBorder(toolbar, nil, nil, nil, i.content)
	return nil
}

func (i *index) buyBatchDialog(depthStr, amountStr *string) fyne.CanvasObject {

	depthBind := binding.BindString(depthStr)
	amountBind := binding.BindString(amountStr)

	amountEntry := widget.NewEntryWithData(amountBind)

	depthSlider := widget.NewSlider(float64(18), float64(30))
	depthSlider.Step = 2.0
	depthSlider.OnChanged = func(f float64) {
		depthBind.Set(fmt.Sprintf("%d", int64(f)))
	}

	optionsForm := widget.NewForm()
	optionsForm.Append(
		"Depth",
		container.NewMax(depthSlider),
	)

	optionsForm.Append(
		"Amount",
		container.NewMax(amountEntry),
	)

	return container.NewMax(optionsForm)
}

func (i *index) refDialog(ref string) fyne.CanvasObject {
	refButton := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
		i.Window.Clipboard().SetContent(ref)
	})
	optionsForm := widget.NewForm()
	optionsForm.Append(
		"Reference",
		container.NewBorder(nil, nil, nil, widget.NewLabel(shortenHashOrAddress(ref)), refButton),
	)

	return container.NewMax(optionsForm)
}

func (i *index) showProgressWithMessage(message string) {
	i.progress = dialog.NewProgressInfinite("", message, i) //lint:ignore SA1019 fyne-io/fyne/issues/2782
	i.progress.Show()
}

func (i *index) hideProgress() {
	i.progress.Hide()
}

func (i *index) showError(err error) {
	label := widget.NewLabel(err.Error())
	label.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustom("Error", "OK", label, i.Window)
	parentSize := i.Window.Canvas().Size()
	d.Resize(fyne.NewSize(parentSize.Width*90/100, 0))
	d.Show()
}

func (i *index) showErrorWithAddr(addr common.Address, err error) {
	addrStr := shortenHashOrAddress(addr.String())
	addrCopyButton := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
		i.Window.Clipboard().SetContent(addr.String())
	})
	header := container.NewHBox(widget.NewLabel(addrStr), addrCopyButton)
	label := widget.NewLabel(err.Error())
	label.Wrapping = fyne.TextWrapWord
	content := container.NewBorder(header, label, nil, nil)
	d := dialog.NewCustom("Error", "OK", content, i.Window)
	parentSize := i.Window.Canvas().Size()
	d.Resize(fyne.NewSize(parentSize.Width*90/100, 0))
	d.Show()
}

func shortenHashOrAddress(item string) string {
	return fmt.Sprintf("%s[...]%s", item[0:6], item[len(item)-6:len(item)])
}
