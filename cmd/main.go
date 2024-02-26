package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// TODO доработать селекторы, они должны быть более универсальными
// TODO добавить логирование

const (
	siteUrl      = `https://sbermarket.ru`
	addressStore = "2-я Владимирская улица, 26к1"
)

// Структура для хранения информации о товаре
type ProductInfo struct {
	StoreURL      string
	Category      string
	ProductURL    string
	Name          string
	ImageURL      string
	LargeImageURL string // Может потребоваться перейти на страницу продукта
	Price         string
	OriginalPrice string // Цена до скидки
}

func main() {
	// Настройка параметров запуска Chrome без headless режима и с прокси
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true), // false - запускать браузер в видимом режиме
		chromedp.WindowSize(1920, 1080), // установка разрешения экрана
		// chromedp.ProxyServer("http://91.233.223.147:3128"), // Здесь адрес прокси-сервера(для большего количества запросов)
		// chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.36"),
	)

	// Создание контекста Allocator с нашими параметрами
	ctx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	// Создание контекста Chrome с логированием
	ctx, cancel = chromedp.NewContext(ctx, chromedp.WithLogf(log.Printf))
	defer cancel()

	// Установка таймаута
	ctx, cancel = context.WithTimeout(ctx, 360*time.Second)
	defer cancel()

	log.Printf("Начало навигации по сайту")
	err := chromedp.Run(ctx,
		browser.GrantPermissions([]browser.PermissionType{browser.PermissionTypeGeolocation}).WithOrigin(siteUrl),
		chromedp.Navigate(siteUrl),
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Завершена навигация по сайту")

	var exists bool

	// Создание контекста с таймаутом для первого модального окна
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel() // Гарантируем, что таймер отмены будет вызван, чтобы освободить ресурсы

	// Проверка и закрытие первого модального окна, если оно есть
	selectorFirstModal := `body > div.Modal_root__txmvv.FirstPromoModal_modal__NHjaj > div > div:nth-child(2) > header > button`
	err = chromedp.Run(ctxWithTimeout, chromedp.QueryAfter(selectorFirstModal, func(ctx context.Context, id runtime.ExecutionContextID, node ...*cdp.Node) error {
		exists = len(node) > 0
		return nil
	}, chromedp.ByQuery))
	if err == nil && exists {
		err = chromedp.Run(ctx, chromedp.Click(selectorFirstModal, chromedp.ByQuery))
		if err != nil {
			log.Println("Ошибка при закрытии первого модального окна:", err)
		}
	}

	// Создание контекста с таймаутом для второго модального окна
	ctxWithTimeout2, cancel2 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel2() // Гарантируем, что таймер отмены будет вызван, чтобы освободить ресурсы

	// Проверка и закрытие второго модального окна, если оно есть
	selectorSecondModal := `#__next > div.styles_framesGroup__iZc0Z > div.styles_frame__laheH > div > div > div:nth-child(2) > div > div > div.ModalWrapper_right__NxZN0 > button`
	err = chromedp.Run(ctxWithTimeout2, chromedp.QueryAfter(selectorSecondModal, func(ctx context.Context, id runtime.ExecutionContextID, node ...*cdp.Node) error {
		exists = len(node) > 0
		return nil
	}, chromedp.ByQuery))
	if err == nil && exists {
		err = chromedp.Run(ctx, chromedp.Click(selectorSecondModal, chromedp.ByQuery))
		if err != nil {
			log.Println("Ошибка при закрытии второго модального окна:", err)
		}
	}

	// Ожидаем появления поля ввода адреса
	err = chromedp.Run(ctx, chromedp.WaitVisible(`#by_courier > div.MainBanner_root__7uO0e.MainBanner_background__IiBe2.Address_banner__0VXxz.HomeLanding_banner__h1Y1D > div > div.Address_inputWrapper__g_O6L > div > div > input`, chromedp.ByID))
	if err != nil {
		log.Println(err)
	}

	// Ввод адреса (для выбора магазинов, из которых есть доставка по этому адресу) и нажатие Enter
	err = chromedp.Run(ctx,
		chromedp.SendKeys(`#by_courier > div.MainBanner_root__7uO0e.MainBanner_background__IiBe2.Address_banner__0VXxz.HomeLanding_banner__h1Y1D > div > div.Address_inputWrapper__g_O6L > div > div > input`, addressStore, chromedp.ByID),
		chromedp.Sleep(2*time.Second), // Небольшая задержка для убеждения, что текст введен полностью
		chromedp.KeyEvent(kb.Enter),
	)
	if err != nil {
		log.Println(err)
	}

	selector, err := waitForEitherSelector(ctx, `#Carousel2Container`, `#__next > div.body > div > div.HomeLanding_pageWrapper__3mybA.HomeLanding_hasNewHeader__M20U5 > div > div.Stores_root__Qsjv1.HomeLanding_section__Z04e1.HomeLanding_sectionRteMode__2CudG > div`)
	if err != nil {
		log.Printf("Не удалось найти ни один из элементов: %v", err)
		return // Выход из функции в случае ошибки
	}

	var storeLinks []string
	var evalStr string

	// Извлечение ссылок на магазины в зависимости от найденного селектора
	if selector == `#Carousel2Container` {
		evalStr = `Array.from(document.querySelectorAll('a.StoreCompact_root__qgCI1')).map(a => a.href);`
	} else {
		evalStr = `Array.from(document.querySelectorAll('a.Stores_storeWrapper__Q6wfJ')).map(a => a.href);`
	}

	// Извлечение ссылок на магазины
	err = chromedp.Run(ctx, chromedp.Evaluate(evalStr, &storeLinks))
	if err != nil {
		log.Println("Ошибка при извлечении ссылок на магазины: ", err)
		return // Выход из функции в случае ошибки
	}

	// Извлечь идентификаторы первых двух магазинов из их URL
	storeIds := make([]string, len(storeLinks))
	for i, link := range storeLinks {
		u, err := url.Parse(link)
		if err != nil {
			log.Fatal(err)
		}
		segments := strings.Split(u.Path, "/")
		if len(segments) > 2 {
			storeIds[i] = segments[2] // второй сегмент (/stores/->177<-)
		}
	}

	allProducts := make([]ProductInfo, 0)

	// Перебираем идентификаторы извлеченных магазинов и переходим на их страницы
	for _, storeId := range storeIds {
		storeUrl := fmt.Sprintf("https://sbermarket.ru/stores/%s?referrer=landing_retailer_list", storeId)

		// Переходим на страницу магазина
		err := chromedp.Run(ctx, chromedp.Navigate(storeUrl))
		if err != nil {
			log.Printf("Ошибка при переходе к магазину с ID %s: %v", storeId, err)
			continue
		}

		// Ожидание загрузки каталога магазина
		err = chromedp.Run(ctx, chromedp.WaitVisible(`#__next > div.body > div:nth-child(4) > section:nth-child(2) > div > ul > div.CategoryGrid_root__6xVIs`, chromedp.ByQuery))
		if err != nil {
			log.Printf("Каталог магазина %s не загрузился: %v", storeId, err)
			continue
		}

		// Получаем все ссылки на категории
		var categoryLinks []string
		err = chromedp.Run(ctx, chromedp.Evaluate(`
            [...document.querySelectorAll('.CategoryGridItem_root__jXxOA .CategoryCard_root__LiY3P')].map(link => link.getAttribute('href'));
        `, &categoryLinks))
		if err != nil {
			log.Printf("Ошибка при получении ссылок каталога: %v", err)
			continue
		}

		// Ограничиваем количество категорий до трех
		if len(categoryLinks) > 3 {
			categoryLinks = categoryLinks[:3]
		}

		// Перебираем выбранные категории
		for _, link := range categoryLinks {
			fullLink := "https://sbermarket.ru" + link
			// Переходим по ссылке текущей категории
			err = chromedp.Run(ctx, chromedp.Navigate(fullLink))
			if err != nil {
				log.Printf("Ошибка при переходе по ссылке категории: %v", err)
				continue
			}

			var products []ProductInfo
			err = chromedp.Run(ctx, chromedp.Evaluate(`
            [...document.querySelectorAll('.ProductCard_root__K6IZK')].map(element => {
                let imgElement = element.querySelector('.ProductCard_image__3jwTC');
                let srcset = imgElement ? imgElement.srcset : '';
                let largeImageURL = '';

                if (srcset) {
                    let urls = srcset.split(', ').map(part => part.trim());
                    let largeImgPart = urls.find(u => u.includes(' 2x'));
                    if (largeImgPart) {
                        largeImageURL = largeImgPart.split(' ')[0]; // Получаем URL до пробела
                    }
                }

                return {
                    storeURL: window.location.href,
                    productURL: element.querySelector('.ProductCardLink_root__69qxV')?.href || '',
                    name: element.querySelector('.ProductCard_title__iNsaD')?.textContent || '',
                    imageURL: imgElement ? imgElement.src : '',
                    largeImageURL: largeImageURL, // Добавляем URL большого изображения
                    price: element.querySelector('.ProductCardPrice_price__Kv7Q7')?.textContent || '',
                    originalPrice: element.querySelector('.ProductCardPrice_originalPrice__z36Di')?.textContent || ''
                };
            });
        `, &products))

			if err != nil {
				log.Printf("Ошибка при извлечении данных о продуктах: %v", err)
				continue
			}

			allProducts = append(allProducts, products...)
		}
	}

	// Запись данных в CSV-файл
	file, err := os.Create("products1.csv")
	if err != nil {
		log.Fatalf("Не удалось создать файл: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Установка кастомного разделителя
	writer.Comma = ';' // Используем точку с запятой в качестве разделителя

	// Установка кавычек для всех полей
	writer.UseCRLF = true // Используем в Windows-стиле концы строк

	// Заголовки для CSV-файла
	headers := []string{"Store URL", "Category", "Product URL", "Name", "Image URL", "Large Image URL", "Price", "Original Price"}
	err = writer.Write(headers)
	if err != nil {
		log.Fatalf("Не удалось записать заголовки в CSV-файл: %v", err)
	}

	// Запись данных продуктов
	for _, product := range allProducts {
		record := []string{
			clean(product.StoreURL),
			clean(product.Category),
			clean(product.ProductURL),
			clean(product.Name),
			clean(product.ImageURL),
			clean(product.LargeImageURL),
			clean(product.Price),
			clean(product.OriginalPrice),
		}
		err := writer.Write(record)
		if err != nil {
			log.Fatalf("Не удалось записать данные о продукте в CSV-файл: %v", err)
		}
	}

}

// Функция для очистки значений от переносов строк и других потенциально проблемных символов
func clean(value string) string {
	// Замените \n, \r и \r\n на пробелы или другой подходящий символ
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	// Дополнительно можно удалить двойные кавычки или экранировать их
	value = strings.ReplaceAll(value, "\"", "\"\"")
	return value
}

func waitForEitherSelector(ctxForEitherSelector context.Context, selectors ...string) (string, error) {
	ctxForEitherSelector, cancelCtx := context.WithCancel(ctxForEitherSelector) // Создаем контекст с возможностью отмены
	defer cancelCtx()

	// Канал для передачи успешно найденного селектора
	found := make(chan string)

	// Запускаем горутину для каждого селектора
	for _, sel := range selectors {
		go func(selector string) {
			if err := chromedp.Run(ctxForEitherSelector, chromedp.WaitReady(selector)); err == nil {
				found <- selector // Если элемент готов, отправляем селектор в канал
			}
		}(sel)
	}

	select {
	case selector := <-found:
		return selector, nil // Возвращаем первый найденный селектор
	case <-ctxForEitherSelector.Done():
		return "", ctxForEitherSelector.Err() // Время ожидания истекло
	}
}
