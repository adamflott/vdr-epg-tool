/* vdr-epg-tool

About:
Loads XMLTV (xmltv.org) output into VDR's EPG (electronic program
guide http://en.wikipedia.org/wiki/Electronic_program_guide). Almost
a straight port of xmltv2vdr.pl
(https://github.com/rdaoc/xmltv2vdr/blob/master/xmltv2vdr.pl) into
Go.

Features over xmltv2vdr.pl:

  * No need to modify VDR's channel.conf to add the XMLTV Id !!
  * No external dependencies once static binary built
  * XML parser not based on regular expressions
  * No genre, rating, etc files needed

Author: Adam Flott
Contact: adam@adamflott.com
*/
package main

import (
    "bufio"
    "bytes"
    "encoding/xml"
    "fmt"
    "io"
    "log"
    "net"
    "os"
    "runtime"
    "strconv"
    "strings"
    "time"
)

import (
    "github.com/voxelbrain/goptions"
)

// begin steal from: http://stackoverflow.com/questions/6002619/unmarshal-an-iso-8859-1-xml-input-in-go
type CharsetISO88591er struct {
    r   io.ByteReader
    buf *bytes.Buffer
}

func NewCharsetISO88591(r io.Reader) *CharsetISO88591er {
    buf := bytes.Buffer{}
    return &CharsetISO88591er{r.(io.ByteReader), &buf}
}

func (cs *CharsetISO88591er) Read(p []byte) (n int, err error) {
    for _ = range p {
        if r, err := cs.r.ReadByte(); err != nil {
            break
        } else {
            cs.buf.WriteRune(rune(r))
        }
    }
    return cs.buf.Read(p)
}

func isCharset(charset string, names []string) bool {
    charset = strings.ToLower(charset)
    for _, n := range names {
        if charset == strings.ToLower(n) {
            return true
        }
    }
    return false
}

func IsCharsetISO88591(charset string) bool {
    // http://www.iana.org/assignments/character-sets
    // (last updated 2010-11-04)
    names := []string{
        // Name
        "ISO_8859-1:1987",
        // Alias (preferred MIME name)
        "ISO-8859-1",
        // Aliases
        "iso-ir-100",
        "ISO_8859-1",
        "latin1",
        "l1",
        "IBM819",
        "CP819",
        "csISOLatin1",
    }
    return isCharset(charset, names)
}

func CharsetReader(charset string, input io.Reader) (io.Reader, error) {
    if IsCharsetISO88591(charset) {
        return NewCharsetISO88591(input), nil
    }
    return input, nil
}

// end steal from: http://stackoverflow.com/questions/6002619/unmarshal-an-iso-8859-1-xml-input-in-go

var l *log.Logger
var dl *log.Logger

// xmltv XML types
type Channel struct {
    Id    string   `xml:"id,attr"`
    Names []string `xml:"display-name"`
}

type Programme struct {
    Start       string   `xml:"start,attr"`
    Stop        string   `xml:"stop,attr"`
    Channel     string   `xml:"channel,attr"`
    Title       string   `xml:"title"`
    SubTitle    string   `xml:"sub-title"`
    Description string   `xml:"desc"`
    Credits     string   `xml:"credits"`
    Date        string   `xml:"date"`
    Categories  []string `xml:"category"`
    Rating      string   `xml:"rating>value"`
}

const (
    VDR_SC_HELP                   = 214
    VDR_SC_EPG_DATA_REC           = 215
    VDR_SC_SERVICE_READY          = 220
    VDR_SC_SERVICE_CLOSING        = 221
    VDR_SC_ACTION_OK              = 250
    VDR_SC_EPG_START_SENDING      = 354
    VDR_SC_ACTION_ABORTED         = 451
    VDR_SC_SYNTAX_ERR_CMD_UNREC   = 500
    VDR_SC_SYNTAX_ERR_PARAM_UNREC = 501
    VDR_SC_ERR_CMD_NOT_IMPLE      = 504
    VDR_SC_ACTION_NOT_TAKEN       = 550
    VDR_SC_TRANS_FAILED           = 554
)

var vdr_status_codes map[int]string = map[int]string{
    214: "Help message",
    215: "EPG data record",
    220: "VDR service ready",
    221: "VDR service closing transmission channel",
    250: "Requested VDR action okay, completed",
    354: "Start sending EPG data",
    451: "Requested action aborted: local error in processing",
    500: "Syntax error, command unrecognized",
    501: "Syntax error in parameters or arguments",
    502: "Command not implemented",
    504: "Command parameter not implemented",
    550: "Requested action not taken",
    554: "Transaction failed",
}

var genres map[string]int = map[string]int{
    //EVCONTENTMASK_MOVIEDRAMA
    "Movie/Drama":                    0x10,
    "Action":                         0x10,
    "Detective/Thriller":             0x11,
    "Adventure/Western/War":          0x12,
    "Science Fiction/Fantasy/Horror": 0x13,
    "Comedy":                   0x14,
    "Soap/Melodrama/Folkloric": 0x15,
    "Romance":                  0x16,
    "Serious/Classical/Religious/Historical Movie/Drama": 0x17,
    "Adult Movie/Drama":                                  0x18,
    "Adults only":                                        0x18,
    "Comedy-drama":                                       0x14,
    "Crime drama":                                        0x10,
    "Drama":                                              0x10,
    "Film":                                               0x10,
    "Science fiction":                                    0x13,
    "Soap":                                               0x15,
    "Standup":                                            0x14,

    //  EVCONTENTMASK_NEWSCURRENTAFFAIRS,
    "News/Current Affairs":        0x20,
    "News/Weather Report":         0x21,
    "News Magazine":               0x22,
    "Documentary":                 0x23,
    "Discussion/Inverview/Debate": 0x24,
    "Weather":                     0x21,

    //  EVCONTENTMASK_SHOW,
    "Show/Game Show":         0x30,
    "Game Show/Quiz/Contest": 0x31,
    "Variety Show":           0x32,
    "Talk Show":              0x33,

    //  EVCONTENTMASK_SPORTS,
    "Sports":          0x40,
    "Action sports":   0x40,
    "Special Event":   0x41,
    "Sport Magazine":  0x42,
    "Football/Soccer": 0x43,
    "Tennis/Squash":   0x44,
    "Team Sports":     0x45,
    "Athletics":       0x46,
    "Motor Sport":     0x47,
    "Water Sport":     0x48,
    "Winter Sports":   0x49,
    "Equestrian":      0x4A,
    "Martial Sports":  0x4B,
    "Archery":         0x46,
    "Baseball":        0x45,
    "Basketball":      0x45,
    "Bicycle":         0x40,
    "Boxing":          0x40,
    "Billiards":       0x40,

    //  EVCONTENTMASK_CHILDRENYOUTH,
    "Children's/Youth Programme":                 0x50,
    "Pre-school Children's Programme":            0x51,
    "Entertainment Programme for 6 to 14":        0x52,
    "Entertainment Programme for 10 to 16":       0x53,
    "Informational/Educational/School Programme": 0x54,
    "Cartoons/Puppets":                           0x55,
    "Paid Programming":                           0x54,

    //  EVCONTENTMASK_MUSICBALLETDANCE,
    "Music/Ballet/Dance":      0x60,
    "Rock/Pop":                0x61,
    "Serious/Classical Music": 0x62,
    "Folk/Tradional Music":    0x63,
    "Jazz":                    0x64,
    "Musical/Opera":           0x65,
    "Ballet":                  0x66,

    //  EVCONTENTMASK_ARTSCULTURE,
    "Arts/Culture":                     0x70,
    "Performing Arts":                  0x71,
    "Fine Arts":                        0x72,
    "Religion":                         0x73,
    "Religous":                         0x73,
    "Popular Culture/Traditional Arts": 0x74,
    "Literature":                       0x75,
    "Film/Cinema":                      0x76,
    "Experimental Film/Video":          0x77,
    "Broadcasting/Press":               0x78,
    "New Media":                        0x79,
    "Arts/Culture Magazine":            0x7A,
    "Fashion":                          0x7B,
    "Arts/crafts":                      0x70,

    //  EVCONTENTMASK_SOCIALPOLITICALECONOMICS,
    "Social/Political/Economics":  0x80,
    "Magazine/Report/Documentary": 0x81,
    "Economics/Social Advisory":   0x82,
    "Remarkable People":           0x83,

    //  EVCONTENTMASK_EDUCATIONALSCIENCE,
    "Education/Science/Factual":      0x90,
    "Nature/Animals/Environment":     0x91,
    "Technology/Natural Sciences":    0x92,
    "Medicine/Physiology/Psychology": 0x93,
    "Foreign Countries/Expeditions":  0x94,
    "Social/Spiritual Sciences":      0x95,
    "Further Education":              0x96,
    "Languages":                      0x97,

    //  EVCONTENTMASK_LEISUREHOBBIES,
    "Leisure/Hobbies":        0xA0,
    "Tourism/Travel":         0xA1,
    "Handicraft":             0xA2,
    "Motoring":               0xA3,
    "Fitness & Health":       0xA4,
    "Cooking":                0xA5,
    "Advertisement/Shopping": 0xA6,
    "Gardening":              0xA7,
    "Aerobics":               0xA4,

    //  EVCONTENTMASK_SPECIAL,
    "Original Language": 0xB1,
    "Black & White":     0xB2,
    "Unpublished":       0xB3,
    "Live Broadcast":    0xB4,

    // Below you can add your own category's if the xml file does not provide the right names,
    "Children":      0x50,
    "Animated":      0x50,
    "Crime/Mystery": 0x11,
    //	"Drama"  : 0x15,
    "Educational":    0x90,
    "Science/Nature": 0x91,
    "Adult":          0x18,
    //"Film"  : 0x10,
    "Music":     0x60,
    "News":      0x20,
    "Talk":      0x33,
    "Unknown":   0x0,
    "Anime":     0x50,
    "Animation": 0x50,
}

var ratings map[string]int = map[string]int{
    "TV-Y":  2,
    "TV-Y7": 7,
    "TV-G":  8,
    "TV-PG": 10,
    "TV-14": 14,
    "TV-MA": 18,
}

// VDR types
type VDRChannel struct {
    Name        string
    Aliases     []string
    CallSign    string
    Number      string
    Frequency   string
    Param       string
    Source      string
    Srate       string
    VPID        string
    APID        string
    TPID        string
    CondAccess  string
    ServiceId   string
    NetworkId   string
    TransportId string
    RadioId     string
}

var channels map[string]VDRChannel

type VDREPGEvent struct {
    CChannel        string
    ChannelCallSign string
    EEventId        uint64
    EEStartTime     string
    EEStopTime      string
    EEDuration      string
    TTitle          string
    SSubTitle       string
    DDescription    string
    GGenres         []int
    RRating         int
}

func d(prefix string, format string, a ...interface{}) {
    pc, _, line, _ := runtime.Caller(1)
    msg := fmt.Sprintf(format, a...)
    dl.Printf("debug %s %s:%d %v", prefix, runtime.FuncForPC(pc).Name(), line, msg)
}

func svdrp_write(conn net.Conn, format string, a ...interface{}) {
    d("svdrp", "sending '%s'", fmt.Sprintf(format, a...))
    cmd := fmt.Sprintf(format+"\r\n", a...)
    fmt.Fprintf(conn, cmd)
}

func svdrp_wait_for_reply(conn net.Conn, reply int) {
    r := bufio.NewReader(conn)
    d("svdrp", "waiting for reply '%d' (%s)", reply, vdr_status_codes[reply])
    data, err := r.ReadString('\n')

    if err != nil {
        l.Fatalln("svdrp: read error", err)
    }

    status := data[0:3]
    replystr := strconv.FormatInt(int64(reply), 10)
    if err != nil || status != replystr {
        d("svdrp", "status=%s; error=%s; data=%s", status, err, data)
        l.Fatalf("svdrp: vdr reply code (%s) didn't match expected (%d, %s)", status, reply, vdr_status_codes[reply])
    }
    d("svdrp", "got reply: %s", replystr)
}

func svdrp_write_n_reply(conn net.Conn, cmd string, reply int) {
    svdrp_write(conn, "%s", cmd)
    svdrp_wait_for_reply(conn, reply)
}

func load_vdr_channels(file *os.File) (channels map[string]VDRChannel) {
    // channels.conf format: ABC,WCVB:509028:M10:A:0:49=2:0:0:0:3:0:0:0

    channels = make(map[string]VDRChannel)

    defer file.Close()

    chsScanner := bufio.NewScanner(file)
    for chsScanner.Scan() {

        if strings.HasPrefix(chsScanner.Text(), ":") == true || len(chsScanner.Text()) == 0 {
            continue
        }
        fields := strings.Split(chsScanner.Text(), ":")

        ncs := strings.Split(fields[0], ",")
        if len(ncs) < 2 {
            l.Println("channels.conf: expected 2 fields, format: <vdr name>, <xmltv identifier>")
            continue
        }

        cs := strings.Split(ncs[1], ";")

        ch := VDRChannel{
            Name:        ncs[0],
            CallSign:    cs[0],
            Frequency:   fields[1],
            Param:       fields[2],
            Source:      fields[3],
            Srate:       fields[4],
            VPID:        fields[5],
            APID:        fields[6],
            TPID:        fields[7],
            CondAccess:  fields[8],
            ServiceId:   fields[9],
            NetworkId:   fields[10],
            TransportId: fields[11],
            RadioId:     fields[12],
        }
        channels[cs[0]] = ch
    }
    if err := chsScanner.Err(); err != nil {
        l.Fatalln(err)
    }
    return
}

func vdr_make_channel_id(c VDRChannel) (i string) {

    fq, _ := strconv.Atoi(c.Frequency)

    // this is what xmltv2vdr.pl does, but I have no idea why! the
    // vdr docs don't mention anything
    if c.Source == "A" || c.Source == "T" {
        fq /= 1000
    }

    i = fmt.Sprintf("%s-%s-%d-%s", c.Source, c.NetworkId, fq, c.ServiceId)

    if c.TransportId != "0" || c.NetworkId != "0" {
        i = fmt.Sprintf("%s-%s-%s-%s", c.Source, c.NetworkId, c.TransportId, c.ServiceId)
    }
    return
}

func vdr_epg_load(vdrhost string, netdone chan bool, comm chan VDREPGEvent) {
    conn, cerr := net.Dial("tcp", vdrhost)
    if cerr != nil {
        l.Fatalln("svdrp: connect to", vdrhost, "faild with error:", cerr)
    }

    d("svdrp", "connected to %s", vdrhost)
    svdrp_wait_for_reply(conn, VDR_SC_SERVICE_READY)
    svdrp_write_n_reply(conn, "CLRE", VDR_SC_ACTION_OK)

    done := false

    cur_channel := ""

    nchan := make(map[string]int)

    for done == false {
        select {
        case e, ok := <-comm:

            if ok == false {
                done = true
                break
            }

            if _, fc := channels[e.ChannelCallSign]; fc == false {
                continue
            }
            cmd := ""

            if cur_channel != "" && cur_channel != e.ChannelCallSign {
                svdrp_write(conn, "c")
                svdrp_write_n_reply(conn, ".", VDR_SC_ACTION_OK)
            }

            if cur_channel == "" || cur_channel != e.ChannelCallSign {
                svdrp_write_n_reply(conn, "PUTE", VDR_SC_EPG_START_SENDING)
                cmd += fmt.Sprintf("C %s %s\r\n", vdr_make_channel_id(channels[e.ChannelCallSign]), e.ChannelCallSign)
                cur_channel = e.ChannelCallSign
                nchan[cur_channel]++
            }

            d := e.EEStartTime

            d1, _ := strconv.Atoi(d[0:4])
            d2, _ := strconv.Atoi(d[4:6])
            d3, _ := strconv.Atoi(d[6:8])
            d4, _ := strconv.Atoi(d[8:10])
            d5, _ := strconv.Atoi(d[10:12])
            d6, _ := strconv.Atoi(d[12:14])

            dts := time.Date(d1, time.Month(d2), d3, d4, d5, d6, 0, time.UTC)

            d = e.EEStopTime

            d1, _ = strconv.Atoi(d[0:4])
            d2, _ = strconv.Atoi(d[4:6])
            d3, _ = strconv.Atoi(d[6:8])
            d4, _ = strconv.Atoi(d[8:10])
            d5, _ = strconv.Atoi(d[10:12])
            d6, _ = strconv.Atoi(d[12:14])

            dte := time.Date(d1, time.Month(d2), d3, d4, d5, d6, 0, time.UTC)

            du := dte.Sub(dts)

            eid := dts.Unix() / 60 % 0xffff

            s := e.SSubTitle

            g := ""
            for _, v := range e.GGenres {
                g += strconv.FormatInt(int64(v), 10) + " "
            }

            cmd += fmt.Sprintf("E %d %d %d 0\r\n", eid, dts.Unix(), int(du.Seconds()))
            cmd += fmt.Sprintf("T %s\r\n", e.TTitle)
            if s != "" {
                cmd += fmt.Sprintf("S %s\r\n", s)
            }
            cmd += fmt.Sprintf("D %s\r\n", e.DDescription)
            cmd += fmt.Sprintf("G %s\r\n", g)
            cmd += fmt.Sprintf("R %d\r\n", e.RRating)
            cmd += fmt.Sprintf("e")

            svdrp_write(conn, cmd)

            nchan[cur_channel]++
        }
    }

    svdrp_write(conn, "c")
    svdrp_write_n_reply(conn, ".", VDR_SC_ACTION_OK)
    svdrp_write_n_reply(conn, "QUIT", VDR_SC_SERVICE_CLOSING)

    for k, v := range nchan {
        l.Printf("epg: channel: %s loaded: %d events\n", k, v)
    }

    conn.Close()
    netdone <- true
}

func main() {
    vc, _ := os.Open("/var/lib/vdr/channels.conf")
    xe, _ := os.Open("/var/lib/vdr/xmltv-epg.xml")

    options := struct {
        goptions.Help `goptions:"--help, description='Show this help'"`

        Verbose bool `goptions:"-v, --verbose, description='verbose'"`
        Debug   bool `goptions:"-d, --debug, description='trace execution'"`

        VDRHost string `goptions:"-h, --host, description='host and port'"`

        VDRChannelsFile *os.File `goptions:"-c, --vdr-channels-conf, description='vdrs channels.conf', rdonly"`
        XMLTVEPGFile    *os.File `goptions:"-x, --xmltv-epg-data, description='XMLTV EPG data', rdonly"`

        goptions.Verbs
        EPGLoad struct {
        }   `goptions:"epg-load"`
    }{
        VDRHost:         "127.0.0.1:6419",
        VDRChannelsFile: vc,
        XMLTVEPGFile:    xe,
    }

    goptions.ParseAndFail(&options)
    defer options.XMLTVEPGFile.Close()

    out, _ := os.Open(os.DevNull)
    dout, _ := os.Open(os.DevNull)

    if options.Verbose == true {
        out = os.Stdout
    }

    if options.Debug == true {
        dout = os.Stderr
    }

    l = log.New(out, "", log.Ldate|log.Ltime|log.Lmicroseconds|log.Lshortfile)
    dl = log.New(dout, "", log.Ldate|log.Ltime|log.Lmicroseconds|log.Lshortfile)

    switch string(options.Verbs) {
    case "epg-load":

        channels = load_vdr_channels(options.VDRChannelsFile)
        xmltvid2callsign := make(map[string]string)

        comm := make(chan VDREPGEvent, 1)
        conn := make(chan bool, 1)

        go vdr_epg_load(options.VDRHost, conn, comm)

        decoder := xml.NewDecoder(options.XMLTVEPGFile)
        decoder.CharsetReader = CharsetReader

        for {
            t, err := decoder.Token()
            if t == nil {
                d("XML", "decoding done")
                break
            }

            if err != nil {
                l.Println("XML: decoding error:", err)
                continue
            }

            switch se := t.(type) {
            case xml.StartElement:
                if se.Name.Local == "channel" {
                    var ch Channel
                    decoder.DecodeElement(&ch, &se)

                    for _, name := range ch.Names {

                        if el, found := channels[name]; found == true {
                            el.Aliases = make([]string, len(ch.Names))
                            copy(el.Aliases, ch.Names)
                            xmltvid2callsign[ch.Id] = el.CallSign
                            d("channel", "new channel: %s (%s) (xmltvid: %s)", channels[name].Name, el.CallSign, ch.Id)
                            break
                        }
                    }
                } else if se.Name.Local == "programme" {
                    var p Programme
                    decoder.DecodeElement(&p, &se)

                    var ev VDREPGEvent = VDREPGEvent{
                        CChannel:        p.Channel,
                        ChannelCallSign: xmltvid2callsign[p.Channel],
                        EEStartTime:     p.Start,
                        EEStopTime:      p.Stop,
                        EEDuration:      p.Stop,
                        TTitle:          p.Title,
                        SSubTitle:       p.SubTitle,
                        DDescription:    p.Description,
                        RRating:         ratings[p.Rating],
                    }

                    for _, val := range p.Categories {
                        ev.GGenres = append(ev.GGenres, genres[val])
                    }
                    comm <- ev
                }
            }
        }

        close(comm)

        <-conn
    default:
        goptions.PrintHelp()
        l.Fatalln("command: no command specified")
    }
}
