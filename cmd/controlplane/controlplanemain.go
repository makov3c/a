package main
import (
	"context"
	"log"
	"os"
	"github.com/urfave/cli/v3"
)
func main () {
	cmd := &cli.Command{
		Usage: "control plane for 4a.si/razpravljalnica",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name: "bind",
				Value: "[::]:9870",
				Usage: "url to bind controlplane grpc server to",
			},
			&cli.StringFlag{
				Name: "s2skey",
				Value: "skrivnost",
				Usage: "preshared key za avtentikacijo s2s komunikacije",
			},
			&cli.StringFlag{
				Name: "raftbind",
				Value: "",
				Usage: "url to bind controlplane raft to",
			},
			&cli.StringFlag{
				Name: "raftaddress",
				Value: "",
				Usage: "url of raft to advertise",
			},
			&cli.BoolFlag{
				Name: "bootstrap",
				Value: false,
				Usage: "bootstrap the cluster with this node, to be set on first node.",
			},
			&cli.StringFlag{
				Name: "cluster",
				Value: "",
				Usage: "raft cluster address (for example of one node in a cluster) used to join the raft cluster",
			},
			&cli.StringFlag{
				Name: "myurl",
				Value: "",
				Usage: "my ControlPlane service grpc url, for example localhost:9800",
			},
		},
		Action: func (ctx context.Context, cmd *cli.Command) error {
			if cmd.NArg() > 0 {
				return cli.Exit("Program ne sprejema argumentov. Poženite s stikalom --help za navodila.", 1)
			}
			log.Printf("Main: Poganjam nadzorno ravnino.")
			return ControlPlaneServer(cmd.String("bind"), cmd.String("s2skey"), cmd.String("raftbind"), cmd.String("raftaddress"), cmd.Bool("bootstrap"), cmd.String("cluster"), cmd.String("myurl"))
		},
	}
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
