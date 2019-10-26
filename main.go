package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gobuffalo/packr/v2"
)

var songBox = packr.New("Songs", "./audio/Songs")
var storyBox = packr.New("Stories", "./audio/Stories")
var transitionBox = packr.New("Transitions", "./audio/Transitions")

type audioT struct {
	Name string
	Data *[][]byte
}

var songs []audioT
var stories []audioT
var transitions []audioT
var special = make(map[string]*[][]byte)

var token string // Bot permissions integer 3214336
var vc discordgo.VoiceConnection
var playing = false
var paused = false

var transMap = map[string]string{
	"BlueMoonTransition.dca": "BlueMoon.dca",
	"HeartacheTrans.dca":     "Heartaches.dca",
	"JingleTrans.dca":        "JingleJangle.dca",
	"JohnnyTrans.dca":        "JohnnyGuitar.dca",
	"KickTrans.dca":          "AintThat.dca",
	"LoveMeTrans.dca":        "LoveMe.dca",
	"MadAboutTrans.dca":      "MadAbout.dca",
	"SinTrans.dca":           "SinLie.dca",
	"SomethingsTrans.dca":    "SomethingsGotta.dca",
}

// HELPMESSAGE is the help message
const HELPMESSAGE = "Available Commands:\nHelp - Displays this help message\nJoin - Joins the voice channel\nStop - Leaves voice channel\nPause - Pauses playback\nPlay - Resumes playback"

// JOINVCMESSAGE is the message printed when a user is not in a voice channel
const JOINVCMESSAGE = "You must be in a voice channel to run this command"

// COULDNTSEND is the message printed when bot can't join channel
const COULDNTSEND = "Could not join the channel...is it full?"

// DEBUG enables debug messages
const DEBUG = true

func debug(msg ...interface{}) {
	if DEBUG {
		fmt.Println(msg...)
	}
}

func init() {
	flag.StringVar(&token, "t", "", "Bot Token")
	flag.Parse()
}

func main() {
	if token == "" {
		fmt.Println("No token provided.\nPlease run:", os.Args[0], "-t <bot token>")
		return
	}

	loadAudioFiles()

	discord, err := discordgo.New("Bot " + token)
	if err != nil {
		panic(err)
	}

	discord.AddHandler(messageCreate)

	if err := discord.Open(); err != nil {
		panic(err)
	}

	fmt.Println("Running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	discord.Close()
}

func loadSound(b []byte) *[][]byte {
	var opuslen int16
	buffer := make([][]byte, 0)
	reader := bytes.NewReader(b)

	for {
		err := binary.Read(reader, binary.LittleEndian, &opuslen)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return &buffer
		}

		if err != nil {
			panic(err)
		}

		InBuf := make([]byte, opuslen)
		err = binary.Read(reader, binary.LittleEndian, &InBuf)
		if err != nil {
			panic(err)
		}

		buffer = append(buffer, InBuf)
	}
}

func loadAudioFiles() {
	songList := songBox.List()
	for _, song := range songList {
		bytes, err := songBox.Find(song)
		if err != nil {
			panic(err)
		}
		songs = append(songs, audioT{Name: song, Data: loadSound(bytes)})
	}
	storyList := storyBox.List()
	for _, story := range storyList {
		bytes, err := storyBox.Find(story)
		if err != nil {
			panic(err)
		}
		stories = append(stories, audioT{Name: story, Data: loadSound(bytes)})
	}
	transitionList := transitionBox.List()
	for _, transition := range transitionList {
		bytes, err := transitionBox.Find(transition)
		if err != nil {
			panic(err)
		}
		if transition == "Opening.dca" {
			special["Opening"] = loadSound(bytes)
		} else {
			transitions = append(transitions, audioT{Name: transition, Data: loadSound(bytes)})
		}
	}
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		debug("Message is from bot - ignore")
		return
	}

	if strings.HasPrefix(m.Content, "!vegas") {
		split := strings.Split(m.Content, " ")
		if len(split) < 2 {
			printHelp(s, m)
			return
		}

		command := strings.ToLower(split[1])
		if command == "join" {
			if !playing && !paused {
				loop(s, m)
			}
		} else if command == "stop" {
			if playing {
				stop(s, m)
			}
		} else if command == "pause" {
			if !paused {
				pause(s, m)
			}
		} else if command == "play" {
			if paused {
				play(s, m)
			}
		} else {
			printHelp(s, m)
		}
	}
}

// Commands

func printHelp(s *discordgo.Session, m *discordgo.MessageCreate) {
	s.ChannelMessageSend(m.ChannelID, HELPMESSAGE)
}

func loop(s *discordgo.Session, m *discordgo.MessageCreate) {
	ch, err := s.State.Channel(m.ChannelID)
	if err != nil {
		debug("Could not find channel")
		return
	}

	guild, err := s.State.Guild(ch.GuildID)
	if err != nil {
		debug("Could not find guild")
		return
	}

	found := false
	var vcID string

	for _, vs := range guild.VoiceStates {
		if vs.UserID == m.Author.ID {
			found = true
			vcID = vs.ChannelID
		}
	}

	if !found {
		debug("Could not find user in voice states")
		s.ChannelMessageSend(m.ChannelID, JOINVCMESSAGE)
		return
	}

	vc, err := s.ChannelVoiceJoin(guild.ID, vcID, false, true)
	if err != nil {
		debug("Could not join channel")
		s.ChannelMessageSend(m.ChannelID, COULDNTSEND)
		return
	}

	debug("Speaking")
	vc.Speaking(true)
	playing = true
	i := 0
	var sname string
	for {
		// quick pause
		time.Sleep(250 * time.Millisecond)
		// Get new seed
		rand.Seed(time.Now().UTC().UnixNano())
		if i == 0 {
			if done := playAudio(vc, special["Opening"]); done {
				break
			}
		} else {
			if i%5 == 0 {
				transition := transitions[rand.Intn(len(transitions))]
				if done := playAudio(vc, transition.Data); done {
					break
				}
				if sn, special := transMap[transition.Name]; special {
					sname = sn
				}
			} else if i%4 == 0 {
				story := stories[rand.Intn(len(stories))]
				if done := playAudio(vc, story.Data); done {
					break
				}
			} else {
				if sname != "" {
					var song audioT
					for _, s := range songs {
						if s.Name == sname {
							song = s
							break
						}
					}
					sname = ""
					if done := playAudio(vc, song.Data); done {
						break
					}
				} else {
					song := songs[rand.Intn(len(songs))]
					if done := playAudio(vc, song.Data); done {
						break
					}
				}
			}
		}

		i++
	}

	vc.Speaking(false)
	debug("Done Speaking")
	vc.Disconnect()
}

func playAudio(vc *discordgo.VoiceConnection, data *[][]byte) bool {
	for _, buff := range *data {
		if !playing {
			return true
		} else if paused {
			for {
				time.Sleep(250 * time.Millisecond)
				if !playing {
					return true
				}
				if !paused {
					break
				}
			}
		}
		vc.OpusSend <- buff
	}
	return false
}

func stop(s *discordgo.Session, m *discordgo.MessageCreate) {
	ch, err := s.State.Channel(m.ChannelID)
	if err != nil {
		debug("Could not find channel")
		return
	}

	guild, err := s.State.Guild(ch.GuildID)
	if err != nil {
		debug("Could not find guild")
		return
	}

	found := false

	for _, vs := range guild.VoiceStates {
		if vs.UserID == m.Author.ID {
			found = true
		}
	}

	if !found {
		debug("Could not find user in voice states")
		s.ChannelMessageSend(m.ChannelID, JOINVCMESSAGE)
		return
	}

	playing = false
	paused = false
}

func pause(s *discordgo.Session, m *discordgo.MessageCreate) {
	ch, err := s.State.Channel(m.ChannelID)
	if err != nil {
		debug("Could not find channel")
		return
	}

	guild, err := s.State.Guild(ch.GuildID)
	if err != nil {
		debug("Could not find guild")
		return
	}

	found := false

	for _, vs := range guild.VoiceStates {
		if vs.UserID == m.Author.ID {
			found = true
		}
	}

	if !found {
		debug("Could not find user in voice states")
		s.ChannelMessageSend(m.ChannelID, JOINVCMESSAGE)
		return
	}

	paused = true
}

func play(s *discordgo.Session, m *discordgo.MessageCreate) {
	ch, err := s.State.Channel(m.ChannelID)
	if err != nil {
		debug("Could not find channel")
		return
	}

	guild, err := s.State.Guild(ch.GuildID)
	if err != nil {
		debug("Could not find guild")
		return
	}

	found := false

	for _, vs := range guild.VoiceStates {
		if vs.UserID == m.Author.ID {
			found = true
		}
	}

	if !found {
		debug("Could not find user in voice states")
		s.ChannelMessageSend(m.ChannelID, JOINVCMESSAGE)
		return
	}

	paused = false
}
