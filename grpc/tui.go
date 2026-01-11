package main

import (
	"fmt"
	"strconv"

	pb "4a.si/razpravljalnica/grpc/protobufRazpravljalnica"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"google.golang.org/protobuf/types/known/emptypb"
)

func StartTUI(addr string) {
	app := tview.NewApplication()

	client, err := NewClient(addr)
	if err != nil {
		panic(err)
	}

	loginForm := tview.NewForm()
	topicsList := tview.NewList()
	messagesList := tview.NewList()
	inputField := tview.NewInputField().SetLabel("Message: ")

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

	loginForm.
		AddInputField("User ID", "", 10, tview.InputFieldInteger, nil).
		AddButton("Login", func() {
			idStr := loginForm.GetFormItemByLabel("User ID").(*tview.InputField).GetText()
			id, _ := strconv.ParseInt(idStr, 10, 64)

			client.Login(id)
			statusBar.SetText(fmt.Sprintf("Logged in as user %d", id))

			loadTopics(app, client, topicsList)
			app.SetRoot(root, true).SetFocus(topicsList)
		})

	loginForm.SetBorder(true).SetTitle("Login")

	var currentTopicID int64

	topicsList.SetSelectedFunc(func(index int, mainText, secondary string, shortcut rune) {

		if index == 0 {
			showAddTopicModal(app, client, topicsList, root)
			return
		}

		currentTopicID, _ = strconv.ParseInt(secondary, 10, 64)
		loadMessages(client, messagesList, currentTopicID)
		app.SetFocus(inputField)
	})

	inputField.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			if currentTopicID != 0 {
				text := inputField.GetText()
				if text != "" {
					client.PostMessage(currentTopicID, text)
					inputField.SetText("")
					loadMessages(client, messagesList, currentTopicID)
				}
			}
		case tcell.KeyEsc:
			app.SetFocus(topicsList)
		}
	})

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlC:
			app.Stop()
			return nil
		case tcell.KeyEsc:
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

	if err := app.SetRoot(loginForm, true).Run(); err != nil {
		panic(err)
	}
}

func loadTopics(app *tview.Application, c *Client, list *tview.List) {
	list.Clear()

	// Add Topic button
	list.AddItem("Add Topic", "", 0, nil)

	res, err := c.api.ListTopics(c.ctx(), &emptypb.Empty{})
	if err != nil {
		return
	}

	for _, t := range res.Topics {
		list.AddItem(t.Name, fmt.Sprintf("%d", t.Id), 0, nil)
	}

	list.SetBorder(true).SetTitle("Topics")
}

func loadMessages(c *Client, list *tview.List, topicID int64) {
	list.Clear()

	res, err := c.api.GetMessages(c.ctx(), &pb.GetMessagesRequest{
		TopicId: topicID,
		Limit:   50,
	})
	if err != nil {
		return
	}

	for _, m := range res.Messages {
		msg := m

		label := fmt.Sprintf(
			"%d | u%d | ❤️ %d | %s",
			msg.Id,
			msg.UserId,
			msg.Likes,
			msg.Text,
		)

		list.AddItem(label, " ", 0, func() {
			c.LikeMessage(topicID, msg.Id)
			loadMessages(c, list, topicID)
		})
	}

	list.SetBorder(true).SetTitle("Messages")
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
				c.CreateTopic(name)
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
