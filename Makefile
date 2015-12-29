all:	
	go build -o pre_rogue
	rm -f rogue
	libzipfs-combiner -exe pre_rogue -zip ~/R-3.2.2-install.zip -o rogue
