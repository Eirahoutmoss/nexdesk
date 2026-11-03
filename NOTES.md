**Deneme sürümü — yalnızca Windows.** Uygulama içi güncelleme bu sürümü görmez; `NexDesk.exe`'yi indirip doğrudan çalıştırın. İmzalı tam 1.5.4 sürümü (exe + Android + sunucu) ayrıca yayımlanacak.

### Yenilikler
- **Bağlantıyı kesme karşı tarafa bildiriliyor.** Bir taraf *Bağlantıyı Kes*'e bastığında, pencereyi kapattığında ya da teknisyeni *Kes* ile çıkardığında karşı taraf beklemiyor ve yeniden bağlanmayı denemiyor: "Karşı taraf bağlantıyı kesti" deyip oturumu kapatıyor. Ağ kopmalarında otomatik yeniden bağlanma eskisi gibi sürüyor.
- **Ekran gelmiyorsa nedeni görünüyor.** Destek alan bilgisayar ekranı yakalayamazsa teknisyene nedeni gösteriliyor (kilit/UAC ekranı, NexDesk'in ekrandaki oturumda çalışmaması ya da Windows'un görüntü vermemesi). Hata, Windows oturum ve masaüstü bilgisiyle *Bağlantı Günlüğü*'ne yazılıyor; ilk gönderilen ekran karesi de günlükte.
- **Ekran yakalama düzeltildi.** Bazı ekran sürücülerinde (ör. Uzak Masaüstü oturumu) Windows görüntüyü hata vermeden boş döndürüyordu ve ekran hiç gönderilmiyordu. Görüntü artık bu sürücülerde de çalışan yöntemle okunuyor.
- **Daha net görüntü.** Pencereye sığdırılan uzak ekran artık yumuşatılarak küçültülüyor; küçük yazılar okunaklı.
- **Daha hızlı ekran aktarımı.** Ekran, teknisyenin penceresinin boyutunda gönderiliyor (daha az veri, daha az işlemci, daha net yazı). Ekran değişirken 20 kare/sn; durgun ekranda yakalama yarı hıza iniyor ve hiç veri gitmiyor.
- **Titreme ve kalıntı parçalar giderildi.** Görüntü artık pencereye birebir çiziliyor; çevresinde eski kare ya da arayüz parçası kalmıyor.
- **Tam ekranda titreme ve uzak imleç düzeltildi.** Tam ekran görünümde ana pencere görüntünün üzerine çizilmiyor; destek alanın fare imleci her zaman görünüyor.
- **Yeni ana ekran (AnyDesk tarzı).** Sayfalar ☰ menüsünde. Ana ekranda yan yana *Destek Al* (kod açılışta kendiliğinden hazır, süresi dolunca yenilenir) ve *Destek Ver*. Altında destek verdiğiniz bilgisayarlar küçük ekran resimleriyle: kayıtlı cihaz tek tıkla bağlanır, kodla bağlanılan bilgisayar için yeni kod sorulur. Küçük resimler ana ekrandan kapatılabilir.
- **Bağlantı daha hızlı kuruluyor.** İlk deneme doğrudan yapılıyor; doğrudan yol kurulamazsa birkaç saniye içinde sunucu (TURN) üzerinden bağlanılıyor. Aynı ağda onaydan sonra ekranın gelmesi eskisine göre çok daha kısa sürmeli.
- **Ana ekran düzeltmeleri.** Kod alanında üst üste binen yazı giderildi; giriş kutuları çerçeveli.
- **Donma giderildi.** Güvenlik duvarı sorusu artık programı kilitlemiyor (kutu arkada kalınca program donmuş görünüyordu).
- **Destek alan tarafta İşlemler:** Dosya Gönder, Pano Gönder, Güvenlik Kodunu Doğrula, Bağlantı Günlüğünü Aç. Günlük ayrıca ☰ menüsünde.
- **Destek alınırken donma.** Ekran paylaşılırken destek alan tarafta ☰ menüsünden bir sayfaya geçilince teknisyenin fare/klavyesi yok sayılıyordu. Oturum sürerken bu sayfalar kapalı.
- **Tek kod.** Aynı bilgisayarda birden fazla NexDesk açıksa yalnızca ilki kendiliğinden kod üretir.
- **Destek alan tarafın bağlanınca kilitlenmesi giderildi.** Bağlantı kurulur kurulmaz destek alan NexDesk tamamen kilitlenebiliyordu (görüntü donuyor, bağlantıyı kesmek de işlemiyordu).
