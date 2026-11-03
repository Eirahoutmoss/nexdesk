**Deneme sürümü — yalnızca Windows.** Uygulama içi güncelleme bu sürümü görmez; `NexDesk.exe`'yi indirip doğrudan çalıştırın. İmzalı tam 1.5.4 sürümü (exe + Android + sunucu) ayrıca yayımlanacak.

### Yenilikler
- **Bağlantıyı kesme karşı tarafa bildiriliyor.** Bir taraf *Bağlantıyı Kes*'e bastığında, pencereyi kapattığında ya da teknisyeni *Kes* ile çıkardığında karşı taraf beklemiyor ve yeniden bağlanmayı denemiyor: "Karşı taraf bağlantıyı kesti" deyip oturumu kapatıyor. Ağ kopmalarında otomatik yeniden bağlanma eskisi gibi sürüyor.
- **Ekran gelmiyorsa nedeni görünüyor.** Destek alan bilgisayar ekranı yakalayamazsa teknisyene nedeni gösteriliyor (kilit/UAC ekranı, NexDesk'in ekrandaki oturumda çalışmaması ya da Windows'un görüntü vermemesi). Hata, Windows oturum ve masaüstü bilgisiyle *Bağlantı Günlüğü*'ne yazılıyor; ilk gönderilen ekran karesi de günlükte.
- **Ekran yakalama düzeltildi.** Bazı ekran sürücülerinde (ör. Uzak Masaüstü oturumu) Windows görüntüyü hata vermeden boş döndürüyordu ve ekran hiç gönderilmiyordu. Görüntü artık bu sürücülerde de çalışan yöntemle okunuyor.
