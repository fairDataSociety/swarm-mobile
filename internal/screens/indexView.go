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

func (*logger) Log(s string) {
	log.Println(s)
}

type index struct {
	fyne.Window

	app          fyne.App
	view         *fyne.Container
	content      *fyne.Container
	title        *widget.Label
	intro        *widget.Label
	progress     dialog.Dialog
	activeWindow string
	mtx          sync.Mutex
	b            *bee.Bee
	logger       *logger
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
		logger: &logger{},
	}
	path := a.Storage().RootURI().Path()
	i.title = widget.NewLabel("Swarm")
	i.intro = widget.NewLabel("Initialise your swarm node with a strong password")
	i.intro.Wrapping = fyne.TextWrapWord
	content := container.NewMax()

	// check if password and endpoint is set
	savedPassword := i.app.Preferences().String("password")
	savedSwapEndpoint := i.app.Preferences().String("SwapEndpoint")
	if savedPassword != "" && savedSwapEndpoint != "" {
		go i.start(path, savedPassword, savedSwapEndpoint)
		content.Objects = []fyne.CanvasObject{
			container.NewBorder(
				widget.NewLabel(""),
				widget.NewLabel(""),
				widget.NewLabel(""),
				widget.NewLabel(""),
				widget.NewLabel("Please wait..."),
			),
		}
		i.content = content
		i.view = container.NewBorder(container.NewVBox(i.title, widget.NewSeparator(), i.intro), nil, nil, nil, content)
		return i.view
	}

	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("Password")
	startButton := widget.NewButton("Next", func() {
		if passwordEntry.Text == "" {
			i.showError(fmt.Errorf("password cannot be blank"))
			return
		}
		i.intro.SetText("Swarm mobile needs a rpc endpoint to start")
		content.Objects = []fyne.CanvasObject{i.showRPCView(path, passwordEntry.Text)}
		content.Refresh()
		return
	})
	startButton.Importance = widget.HighImportance
	content.Objects = []fyne.CanvasObject{container.NewBorder(passwordEntry, startButton, nil, nil)}
	i.content = content
	i.view = container.NewBorder(
		container.NewVBox(i.title, widget.NewSeparator(), i.intro), nil, nil, nil, content)
	return i.view
}

func (i *index) showRPCView(path, password string) fyne.CanvasObject {
	rpcEntry := widget.NewEntry()
	rpcEntry.SetPlaceHolder("RPC Endpoint")

	startButton := widget.NewButton("Start", func() {
		if rpcEntry.Text == "" {
			i.showError(fmt.Errorf("rpc endpoint cannot be blank"))
			return
		}
		// test endpoint is connectable
		eth, err := ethclient.Dial(rpcEntry.Text)
		if err != nil {
			i.logger.Log(fmt.Sprintf("rpc endpoint: %s", err.Error()))
			i.showError(fmt.Errorf("rpc endpoint is invalid or not reachable"))
			return
		}

		// check connection
		_, err = eth.ChainID(context.Background())
		if err != nil {
			i.logger.Log(fmt.Sprintf("rpc endpoint: %s", err.Error()))
			i.showError(fmt.Errorf("rpc endpoint: %s", err.Error()))
			return
		}
		i.start(path, password, rpcEntry.Text)
	})
	startButton.Importance = widget.HighImportance

	return container.NewBorder(rpcEntry, startButton, nil, nil)
}

func (i *index) start(path, password, rpc string) {
	if password == "" {
		i.showError(fmt.Errorf("password cannot be blank"))
		return
	}
	i.showProgressWithMessage("Starting Bee")
	// this runs on testnet
	mainnet := false
	err := i.initSwarm(path, path, "welcome from bee-lite", password, rpc, mainnet, logrus.ErrorLevel)
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
	err = i.loadView()
	if err != nil {
		i.showError(err)
		return
	}
	i.intro.SetText("")
}

func (i *index) initSwarm(keystore, dataDir, welcomeMessage, password, swapEndpoint string, mainnet bool, logLevel logrus.Level) error {
	o := &bee.Options{
		FullNodeMode:             true,
		Keystore:                 keystore,
		DataDir:                  dataDir,
		Addr:                     ":6969",
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

	b, err := bee.Start(o, password)
	if err != nil {
		return err
	}
	i.app.Preferences().SetString("password", password)
	i.b = b
	return err
}

func (i *index) loadView() error {
	addrCopyButton := widget.NewButtonWithIcon("   Copy   ", theme.ContentCopyIcon(), func() {
		i.Window.Clipboard().SetContent(i.b.Addr().String())
	})
	addrHeader := container.NewHBox(widget.NewLabel("Overlay address :"))
	addr := container.NewHBox(
		widget.NewLabel(shortenHashOrAddress(i.b.Addr().String())),
		addrCopyButton,
	)
	addrContent := container.NewVBox(addrHeader, addr)

	stampsHeader := container.NewHBox(widget.NewLabel("Postage stamps :"))
	stampsContent := container.NewVBox()
	stamps := i.b.GetAllBatches()
	radio := widget.NewRadioGroup([]string{}, func(s string) {
		if s == "" {
			i.app.Preferences().SetString("selected_stamp", "")
			i.app.Preferences().SetString("batch", "")
			return
		}
		batches := i.b.GetAllBatches()
		for _, v := range batches {
			stamp := hex.EncodeToString(v.ID())
			if s[0:6] == stamp[0:6] {
				i.app.Preferences().SetString("selected_stamp", s)
				i.app.Preferences().SetString("batch", stamp)
			}
		}
	})
	if len(stamps) == 0 {
		// withdraw
		chequebookBalance, err := i.b.ChequebookBalance()
		if err != nil {
			i.showError(err)
			return err
		}
		i.showProgressWithMessage(fmt.Sprintf("withdrawing %s from chequebook", chequebookBalance.String()))
		tx, err := i.b.ChequebookWithdraw(chequebookBalance)
		if err != nil {
			i.hideProgress()
			i.showError(err)
			return err
		}
		i.logger.Log(fmt.Sprintf("chequebook withdraw transaction : %s", tx.String()))
		i.hideProgress()
		i.showProgressWithMessage("buying stamp")

		// just stand by
		<-time.After(time.Second * 30)

		// buy stamp
		depthStr := "22"
		amountStr := "100000000"
		amount, ok := big.NewInt(0).SetString(amountStr, 10)
		if !ok {
			i.showError(fmt.Errorf("invalid amountStr"))
			return fmt.Errorf("invalid amountStr")
		}
		depth, err := strconv.ParseUint(depthStr, 10, 8)
		if err != nil {
			i.showError(fmt.Errorf("invalid depthStr %s", err.Error()))
			return err
		}

		id, err := i.b.BuyStamp(amount, depth, "", false)
		if err != nil {
			i.hideProgress()
			i.showError(err)
			return err
		}
		i.hideProgress()
		radio.Append(shortenHashOrAddress(hex.EncodeToString(id)))
	} else {
		selectedStamp := i.app.Preferences().String("selected_stamp")
		for _, v := range stamps {
			radio.Append(shortenHashOrAddress(hex.EncodeToString(v.ID())))
		}

		radio.SetSelected(selectedStamp)
	}
	stampsContent = container.NewVBox(stampsHeader, radio)

	infoCard := widget.NewCard("Info", fmt.Sprintf("connected with %d peers", i.b.Topology().Connected), container.NewVBox(addrContent, stampsContent))
	go func() {
		// auto reload
		for {
			select {
			case <-time.After(time.Second * 5):
				if i.b != nil {
					infoCard.SetSubTitle(fmt.Sprintf("connected with %d peers", i.b.Topology().Connected))
				}
			}
		}
	}()
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
			filename := path.Text
			uploadedSrt := i.app.Preferences().String("uploads")
			uploads := []uploadedItem{}
			if uploadedSrt != "" {
				err := json.Unmarshal([]byte(uploadedSrt), &uploads)
				if err != nil {
					i.showError(err)
				}
			}
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
			d := dialog.NewCustomConfirm("Upload successful", "Ok", "Cancel", i.refDialog(ref.String()), func(b bool) {}, i.Window)
			i.progress.Hide()
			d.Show()
		}()
	}
	listButton := widget.NewButton("All Uploads", func() {
		uploadedContent := container.NewVBox()
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
				item := container.NewBorder(label, nil, nil, widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
					i.Window.Clipboard().SetContent(ref)
				}))
				uploadedContent.Add(item)
			}
		}
		child := i.app.NewWindow("Uploaded content")
		child.FixedSize()
		child.SetContent(uploadedContentWrapper)
		child.Show()
	})
	uploadCard := widget.NewCard("Upload", "upload content into swarm", container.NewVBox(upForm, listButton))

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
	downloadCard := widget.NewCard("Download", "download content from swarm", dlForm)
	i.content.Objects = []fyne.CanvasObject{container.NewBorder(nil, nil, nil, nil, container.NewScroll(container.NewGridWithColumns(1, infoCard, uploadCard, downloadCard)))}
	i.content.Refresh()
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
	refButton := widget.NewButtonWithIcon("   Copy    ", theme.ContentCopyIcon(), func() {
		i.Window.Clipboard().SetContent(ref)
	})
	return container.NewMax(container.NewBorder(nil, nil, nil, refButton, widget.NewLabel(shortenHashOrAddress(ref))))
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
	d := dialog.NewCustom("Error", "       Close       ", label, i.Window)
	parentSize := i.Window.Canvas().Size()
	d.Resize(fyne.NewSize(parentSize.Width*90/100, 0))
	d.Show()
}

func (i *index) showErrorWithAddr(addr common.Address, err error) {
	addrStr := shortenHashOrAddress(addr.String())
	addrCopyButton := widget.NewButtonWithIcon("   Copy    ", theme.ContentCopyIcon(), func() {
		i.Window.Clipboard().SetContent(addr.String())
	})
	header := container.NewHBox(widget.NewLabel(addrStr), addrCopyButton)
	label := widget.NewLabel(err.Error())
	label.Wrapping = fyne.TextWrapWord
	content := container.NewBorder(header, label, nil, nil)
	d := dialog.NewCustom("Error", "       Close       ", content, i.Window)
	parentSize := i.Window.Canvas().Size()
	d.Resize(fyne.NewSize(parentSize.Width*90/100, 0))
	d.Show()
}

func shortenHashOrAddress(item string) string {
	return fmt.Sprintf("%s[...]%s", item[0:6], item[len(item)-6:len(item)])
}
