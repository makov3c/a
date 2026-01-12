package main
import (
	"fmt"
	"strconv"
	"log"
	"time"
	"strings"
	pb "4a.si/razpravljalnica/grpc/protobufRazpravljalnica"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/codes"
)
func StartTUI (addr string) {
	app := tview.NewApplication()
	var client *Client
	loginForm := tview.NewForm()
	topicsList := tview.NewList()
	messagesList := tview.NewList().SetWrapAround(false).ShowSecondaryText(false)
	inputField := tview.NewInputField().SetLabel("Compose: ")

	statusBar := tview.NewTextView().
		SetText("Not logged in").
		SetTextColor(tcell.ColorYellow)

	rightPane := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(messagesList, 0, 1, false).
		AddItem(inputField, 1, 0, true)

	mainLayout := tview.NewFlex().
		AddItem(topicsList, 30, 1, true).
		AddItem(rightPane, 0, 2, false)

	root := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(statusBar, 1, 0, false).
		AddItem(mainLayout, 0, 1, true)
	var currentTopicID int64
	var id int64
	id = -1
	currentTopicID = -1
	cplanes := []string{addr}
	loginbuttonfunc := func() {
		go func () {
			var err error
			client, err = NewClientCP(cplanes)
			if err != nil {
				app.QueueUpdateDraw(func(){
					loginForm.AddTextView("NewClientCP ni uspel: ", fmt.Sprintf("%v", err), 0, 2, true, false)
				})
				return
			}
			cplanes = client.cplanes
			if id == -1 {
				idStr := loginForm.GetFormItemByLabel("User ID").(*tview.InputField).GetText()
				id, _ = strconv.ParseInt(idStr, 10, 64)
			}
			err = client.Login(id)
			if err != nil {
				app.QueueUpdateDraw(func(){
					loginForm.AddTextView("Prijava ni uspela", fmt.Sprintf("%v", err), 0, 1, true, false)
				})
				return
			}
			users, err := client.GetUsers()
			if err != nil {
				loginForm.AddTextView("GetUsers failed (poskusi znova): ", fmt.Sprintf("%v", err), 0, 1, true, false)
				return
			}
			app.QueueUpdateDraw(func(){
				statusBar.SetText(fmt.Sprintf("Logged in as %s, Ctrl-M to compose", users[id]))
			})
			loadTopics(app, client, topicsList)
			app.QueueUpdateDraw(func(){
				if currentTopicID == -1 {
					app.SetRoot(root, true).SetFocus(topicsList)
					messagesList.Clear()
					messagesList.SetBorder(true).SetTitle(fmt.Sprintf("Select a topic to load messages"))
				} else {
					messagesList.Clear()
					loadMessages(app, client, messagesList, currentTopicID)
					app.SetRoot(root, true).SetFocus(inputField)
				}
			})
			res, err := client.tail.GetSubscriptionNode(client.ctx(), &pb.SubscriptionNodeRequest{UserId: client.userid, TopicId: []int64{}}) // nonstandard, recimo da prazen topicid seznam pomeni vsi topici
			if err != nil {
				loginForm.AddTextView("GetSubscriptionNode ni uspel: ", fmt.Sprintf("%v", err), 0, 1, true, false)
				app.Stop()
				return
			}
			token := res.SubscribeToken
			subclient, err := NewClient(res.Node.Address)
			subclient.Login(client.userid)
			stream, err := subclient.api.SubscribeTopic(subclient.ctx(), &pb.SubscribeTopicRequest{TopicId: []int64{}, UserId: client.userid, FromMessageId: 0, SubscribeToken: token})
			if err != nil {
				log.Fatal("SubscribeTopic ni uspel", err)
			}
			go func () {
				for {
					dogodek, err := stream.Recv()
					if err != nil {
						// log.Fatal("stream.Recv err: %v", err)
						app.Stop()
						return
					}
					if dogodek.Message.TopicId != currentTopicID && dogodek.Message.TopicId > 0 {
						continue
					}
					loadTopics(app, client, topicsList)
					loadMessages(app, client, messagesList, currentTopicID)
				}
			}()
		}()
	}
	loginForm.
		AddInputField("User ID", "", 10, tview.InputFieldInteger, nil).
		AddButton("Login", loginbuttonfunc).AddInputField("Username", "", 10, nil, nil).
		AddButton("Register", func() {
			go func () {
				var err error
				client, err = NewClientCP(cplanes)
				if err != nil {
					app.QueueUpdateDraw(func(){
						loginForm.AddTextView("NewClientCP ni uspel: ", fmt.Sprintf("%v", err), 0, 1, true, false)
					})
					return
				}
				idStr := loginForm.GetFormItemByLabel("Username").(*tview.InputField).GetText()
				cID, err := client.CreateUser(idStr)
				if err != nil {
					app.QueueUpdateDraw(func(){
						loginForm.AddTextView("Neuspešno: ", fmt.Sprintf("%d", cID), 0, 1, true, false)
					})
					return
				}
				app.QueueUpdateDraw(func(){
					loginForm.AddTextView("Uspelo. Vaš ID je", fmt.Sprintf("%d", cID), 0, 1, true, false)
				})
			}()
		})

	loginForm.SetBorder(true).SetTitle("Login")

	topicsList.SetSelectedFunc(func(index int, mainText, secondary string, shortcut rune) {

		if index == 0 {
			showAddTopicModal(app, client, topicsList, root)
			return
		}

		currentTopicID, _ = strconv.ParseInt(secondary, 10, 64)
		loadMessages(app, client, messagesList, currentTopicID)
		app.SetFocus(inputField)
	})

	inputField.SetDoneFunc(func(key tcell.Key) {
		go func () {
			switch key {
			case tcell.KeyEnter:
				if currentTopicID != 0 {
					text := inputField.GetText()
					if text != "" {
						_, err := client.PostMessage(currentTopicID, text)
						if err != nil {
							app.QueueUpdateDraw(func(){
								loginForm.AddTextView("PostMessage: ", fmt.Sprintf("%v", err), 0, 1, true, false)
							})
							app.Stop()
							return
						}
						inputField.SetText("")
						loadMessages(app, client, messagesList, currentTopicID)
					}
				}
			case tcell.KeyEsc:
				loadTopics(app, client, topicsList)
				app.SetFocus(topicsList)
			}
		}()
	})
	samomor := false
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlC:
			samomor = true
			app.Stop()
			return nil
		case tcell.KeyEsc:
			loadTopics(app, client, topicsList)
			app.SetFocus(topicsList)
			return nil

		case tcell.KeyCtrlL:
			app.SetFocus(messagesList)
			return nil

		case tcell.KeyCtrlM:
			app.SetFocus(inputField)
			return nil
		}
		return event
	})
	for !samomor {
		if err := app.SetRoot(loginForm, true).Run(); err != nil {
			panic(err)
		}
		// loginbuttonfunc()
	}
}

func loadTopics(app *tview.Application, c *Client, list *tview.List) {
	go func () {
		res, err := c.tail.ListTopics(c.ctx(), &emptypb.Empty{})
		if err != nil {
			app.Stop()
			return
		}
		app.QueueUpdateDraw(func() {
			cur := list.GetCurrentItem()
			list.Clear()
			list.ShowSecondaryText(false)
			list.AddItem(" [Add Topic]", "", 0, nil)
			for _, t := range res.Topics {
				list.AddItem(t.Name, fmt.Sprintf("%d", t.Id), 0, nil)
			}
			list.SetBorder(true).SetTitle("Topics. Esc to select.")
			list.SetCurrentItem(cur)
		})
	}()
}

func loadMessages(app *tview.Application, c *Client, list *tview.List, topicID int64) {
	go func () {
		if c == nil {
			return // race race race race !!!!!
		}
		users, err := c.GetUsers()
		if err != nil {
			app.Stop()
			return
		}
		res, err := c.tail.GetMessages(c.ctx(), &pb.GetMessagesRequest{
			TopicId: topicID,
			Limit:   9999,
		})
		app.QueueUpdateDraw(func() {
			list.Clear()
			if err != nil {
				return
			}
			maxuserlen := 0
			for _, v := range users {
				if len(v) > maxuserlen {
					maxuserlen = len(v)
				}
			}
			maxuserlen = min(maxuserlen, 9)
			for _, m := range res.Messages {
				msg := m
				username := users[msg.UserId]
				filler := ""	
				for i := 0; i < maxuserlen-len(username); i++ {
					filler = filler + " "
				}
				label := fmt.Sprintf(
					"%v <%s>%s ❤️ %d | %s",
					msg.CreatedAt.AsTime().Local().Format(time.DateTime),
					username,
					filler,
					msg.Likes,
					msg.Text,
				)
				list.AddItem(label, fmt.Sprintf("%d %d", msg.Id, msg.UserId), 0, func() {
					err := c.LikeMessage(topicID, msg.Id)
					if err != nil {
						st, ok := status.FromError(err)
						if ok && st.Code() == codes.AlreadyExists {
							return // already liked
						}
						app.Stop()
						return
					}
					loadMessages(app, c, list, topicID)
				})
			}
			list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
				switch event.Key() {
					case tcell.KeyDelete:
						index := list.GetCurrentItem()
						_, secondary := list.GetItemText(index)
						parts := strings.SplitN(secondary, " ", 2)
						msgid, _ := strconv.ParseInt(parts[0], 10, 64)
						userid, _ := strconv.ParseInt(parts[1], 10, 64)
						if c.userid != userid {
							return nil
						}
						c.DeleteMessage(msgid)
						loadMessages(app, c, list, topicID)
						return nil
				}
				return event
			})
			list.SetCurrentItem(list.GetItemCount()-1)
			list.SetBorder(true).SetTitle(fmt.Sprintf("%d Messages. Ctrl-L: Select, Enter: Like, Del: Delete", len(res.Messages)))
		})
	}()
}

func showAddTopicModal(
	app *tview.Application,
	c *Client,
	topicsList *tview.List,
	previousRoot tview.Primitive,
) {
	form := tview.NewForm()
	form.
		AddInputField("Topic name", "", 20, nil, nil).
		AddButton("Create", func() {
			name := form.GetFormItem(0).(*tview.InputField).GetText()
			if name != "" {
				_, err := c.CreateTopic(name)
				if err != nil {
					app.Stop()
					return
				}
				loadTopics(app, c, topicsList)
			}
			app.SetRoot(previousRoot, true)
			app.SetFocus(topicsList)
		}).
		AddButton("Cancel", func() {
			app.SetRoot(previousRoot, true)
			app.SetFocus(topicsList)
		})

	form.SetBorder(true).SetTitle("Add Topic")

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(form, 40, 1, true).
		AddItem(nil, 0, 1, false)

	app.SetRoot(modal, true).SetFocus(form)
}
