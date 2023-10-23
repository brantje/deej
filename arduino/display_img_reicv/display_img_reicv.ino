/*********
# Copyright (C) Sander Brand
# This file is part of Deej <https://github.com/brantje/deej>.
#
# deej is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# deej is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with deej.  If not, see <http://www.gnu.org/licenses/>.
*********/

#include <Wire.h>
#include <Adafruit_GFX.h>
#include <Adafruit_SSD1306.h>

#define SCREEN_WIDTH 128
#define SCREEN_HEIGHT 64
#define TCA9548A_ADDRESS 0x70
#define LCD_I2C_ADDRESS 0x3C

const char START_SERIAL_TAG[] = "<<START>>";
const char END_SERIAL_TAG[] = "<<END>>";
const int NUM_DISPLAYS = 5;                                   // Change if your display amount is different
const int DISPLAY_SLIDER_MAP[NUM_DISPLAYS] = {0, 1, 4, 3, 2}; // Slider -> Display
Adafruit_SSD1306 display(SCREEN_WIDTH, SCREEN_HEIGHT, &Wire, -1);
boolean isReceiving = false;
int display_idx = 1;
int x = 0, y = 0;

const int BUFFER_SIZE = 128;
char buffer[BUFFER_SIZE];
int bufferIndex = 0;

enum State
{
  WAITING_FOR_START,
  RECEIVING_DATA,
  WAITING_FOR_END
};
State currentState = WAITING_FOR_START;

// Select I2C BUS
void TCA9548A(uint8_t bus)
{
  Wire.beginTransmission(0x70); // TCA9548A address
  Wire.write(1 << bus);         // send byte to select bus
  Wire.endTransmission();
}

void setup()
{
  Serial.begin(115200);

  // Start I2C communication with the Multiplexer
  Wire.begin();

  for (int i = 0; i < NUM_DISPLAYS; i++)
  {
    initializeDisplay(DISPLAY_SLIDER_MAP[i]);
    showText(i, "Loading", 2);
  }
}

void loop()
{
  recvWithStartEndMarkers();
}

void recvWithStartEndMarkers()
{
  while (Serial.available())
  {
    char c = Serial.read();
    if (bufferIndex < BUFFER_SIZE - 1)
    {
      buffer[bufferIndex] = c;
      bufferIndex++;
    }
    else
    {
      // Reset buffer if overflowed
      bufferIndex = 0;
      memset(buffer, 0, BUFFER_SIZE);
    }

    switch (currentState)
    {
    case WAITING_FOR_START:

      if (bufferIndex >= 9 && strncmp(buffer + bufferIndex - strlen(START_SERIAL_TAG), START_SERIAL_TAG, strlen(START_SERIAL_TAG)) == 0)
      {
        showText(0, "Receiving", 2);
        int display_idx = Serial.readStringUntil('|').toInt();
        currentState = RECEIVING_DATA;
        memset(buffer, 0, BUFFER_SIZE); // Clear buffer
        bufferIndex = 0;
        TCA9548A(DISPLAY_SLIDER_MAP[display_idx]);
        // display.clearDisplay();
      }
      break;

    case RECEIVING_DATA:
      if (bufferIndex >= 7 && strncmp(buffer + bufferIndex - strlen(END_SERIAL_TAG), END_SERIAL_TAG, strlen(END_SERIAL_TAG)) == 0)
      {
        display.display();
        currentState = WAITING_FOR_START;
        isReceiving = false;
        memset(buffer, 0, BUFFER_SIZE); // Clear buffer
        bufferIndex = 0;
        showText(0, "Ok", 2);
        Serial.println("Image drawn on display");
        x = 0;
        y = 0;
      }
      else
      {
        for (int bit = 7; bit >= 0; bit--)
        {
          int pixelValue = (c & (1 << bit)) ? 1 : 0;
          display.drawPixel(x, y, pixelValue);
          x++;
          if (x >= SCREEN_WIDTH)
          {
            x = 0;
            y++;
          }
        }
      }
      break;
    }
  }
}

void initializeDisplay(int display_idx)
{
  TCA9548A(display_idx);
  if (!display.begin(SSD1306_SWITCHCAPVCC, LCD_I2C_ADDRESS))
  {
    Serial.println(F("SSD1306 allocation failed"));
    for (;;)
      ;
  }
  // Clear the buffer
  display.clearDisplay();
}

void drawBitmap(int display_idx, int x, int y, int sx, int sy, unsigned int *data)
{
  int tc = 0;
  for (int Y = 0; Y < sy; Y++)
  {
    for (int X = 0; X < sx; X++)
    {
      display.drawPixel(X + x, Y + y, pgm_read_word(&data[tc]));
      if (tc < (sx * sy))
      {
        tc++;
      }
    }
  }
}

void showText(int display_idx, String msg, int size)
{
  TCA9548A(display_idx);

  int16_t x1;
  int16_t y1;
  uint16_t width;
  uint16_t height;

  display.setTextSize(size);
  display.setTextColor(WHITE);
  display.getTextBounds(msg, 0, 0, &x1, &y1, &width, &height);
  display.clearDisplay(); // clear display
  display.setCursor((SCREEN_WIDTH - width) / 2, (SCREEN_HEIGHT - height) / 2);
  display.println(msg); // text to display
  display.display();
}