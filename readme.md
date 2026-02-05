### Build instructions

go build -o system-walker main.go
sudo setcap 'cap_dac_read_search,cap_net_admin+ep' ./system-walker

### Run the command
./system-walker