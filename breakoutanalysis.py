import pandas as pd
import numpy as np
import matplotlib.pyplot as plt
import seaborn as sns
import requests
import pyotp
import os
import time
from datetime import datetime, timedelta
from tqdm import tqdm
from SmartApi import SmartConnect
from multiprocessing import Pool, cpu_count, Manager
from ratelimit import limits, sleep_and_retry

# Configuration
HOLDING_PERIOD = 21
ENTRY_START_TIME = '09:45'
TOKEN_URL = "https://margincalculator.angelone.in/OpenAPI_File/files/OpenAPIScripMaster.json"
API_DELAY = 1  # 1 second between requests
MAX_RETRIES = 3

# API Configuration
API_KEY = "qPu7eGeX"
CLIENT_CODE = "R586166"
PASSWORD = "1971"
TOTP_SECRET = "AMWHUKFODOPZS5XRJFYEJYGSDI"

# Global variables
SYMBOL_MAP = None
TOKEN_DATA = None

class RateLimiter:
    def __init__(self, counter):
        self.counter = counter
        self.lock = Manager().Lock()

    @sleep_and_retry
    @limits(calls=1, period=API_DELAY)
    def wait(self):
        with self.lock:
            self.counter.value += 1

def fetch_token_mapping():
    global SYMBOL_MAP, TOKEN_DATA
    cache_file = "symbol_tokens.csv"
    
    if not os.path.exists(cache_file):
        print("Downloading symbol token data...")
        response = requests.get(TOKEN_URL)
        TOKEN_DATA = response.json()
        pd.DataFrame(TOKEN_DATA).to_csv(cache_file, index=False)
    else:
        TOKEN_DATA = pd.read_csv(cache_file).to_dict('records')
    
    SYMBOL_MAP = {}
    for item in TOKEN_DATA:
        if 'NSE' in item['exch_seg'] and '-EQ' in item['symbol']:
            symbol = item['symbol'].split('-')[0]
            SYMBOL_MAP[symbol] = str(item['token'])
    return SYMBOL_MAP

def get_symbol_token(symbol):
    if SYMBOL_MAP is None:
        fetch_token_mapping()
    return SYMBOL_MAP.get(symbol, None)

def angel_one_login():
    try:
        smart_api = SmartConnect(api_key=API_KEY)
        totp = pyotp.TOTP(TOTP_SECRET).now()
        session_data = smart_api.generateSession(CLIENT_CODE, PASSWORD, totp)
        return session_data if session_data['status'] else None
    except Exception as e:
        print(f"Login error: {str(e)}")
        return None

def fetch_intraday_entry(symbol_token, breakout_date, rate_limiter):
    try:
        angel_session = angel_one_login()
        if not angel_session:
            return None, None, None, None

        end_date = breakout_date + timedelta(days=HOLDING_PERIOD)
        historicParam = {
            "exchange": "NSE",
            "symboltoken": symbol_token,
            "interval": 'FIVE_MINUTE',
            "fromdate": breakout_date.strftime('%Y-%m-%d') + ' 09:15',
            "todate": end_date.strftime('%Y-%m-%d') + ' 15:30'
        }

        for attempt in range(MAX_RETRIES):
            try:
                rate_limiter.wait()
                data = angel_session['smart_api'].getCandleData(historicParam)
                if data.get('data'):
                    break
            except Exception as e:
                if 'Access denied' in str(e):
                    time.sleep(3)
                    angel_session = angel_one_login()

        df = pd.DataFrame(data['data'], columns=['datetime', 'open', 'high', 'low', 'close', 'volume'])
        df['datetime'] = pd.to_datetime(df['datetime'])
        
        entry_time = datetime.strptime(ENTRY_START_TIME, '%H:%M').time()
        breakout_day_data = df[df['datetime'].dt.date == breakout_date.date()]
        pre_market_high = breakout_day_data[breakout_day_data['datetime'].dt.time < entry_time]['high'].max()
        
        breakout_point = breakout_day_data[(breakout_day_data['datetime'].dt.time >= entry_time) 
                                         & (breakout_day_data['high'] > pre_market_high)].first_valid_index()
        
        if not breakout_point:
            return None, None, None, None
            
        entry_price = df.loc[breakout_point, 'open']
        return entry_price, df.loc[breakout_point:]['high'].tolist(), df.loc[breakout_point:]['low'].tolist(), df.loc[breakout_point:]['close'].tolist()
    
    except Exception as e:
        print(f"Error: {str(e)[:100]}")
        return None, None, None, None

def process_stock(row, progress_queue, rate_limiter):
    try:
        symbol = row['symbol']
        entry_price, highs, lows, closes = fetch_intraday_entry(get_symbol_token(symbol), 
                                                              pd.to_datetime(row['date']), 
                                                              rate_limiter)
        if entry_price:
            return {
                'symbol': symbol,
                'entry_date': row['date'],
                'entry_price': entry_price,
                'highs': highs,
                'lows': lows,
                'closes': closes
            }
    except Exception as e:
        print(f"Error processing {row['symbol']}: {str(e)}")
    finally:
        progress_queue.put(1)

def process_breakouts_with_weekly_filter(df, rate_limiter):
    processed_weeks = {}
    results = []
    
    with tqdm(total=len(df), desc="🔄 Processing Breakouts") as pbar:
        for _, row in df.iterrows():
            week_key = (row['symbol'], pd.to_datetime(row['date']).to_period('W'))
            if week_key not in processed_weeks:
                result = process_stock(row, Manager().Queue(), rate_limiter)
                if result: 
                    results.append(result)
                    processed_weeks[week_key] = True
            pbar.update(1)
    return results

def _evaluate_parameters(args):
    tp_mult, sl_mult, results_subset = args
    metrics = {'returns': []}
    
    for row in results_subset:
        entry = row['entry_price']
        tp = entry * (1 + tp_mult)
        sl = entry * (1 - sl_mult)
        
        for high, low in zip(row['highs'], row['lows']):
            if high >= tp:
                metrics['returns'].append(tp_mult)
                break
            if low <= sl:
                metrics['returns'].append(-sl_mult)
                break
        else:
            metrics['returns'].append((row['closes'][-1]/entry - 1))
    
    sharpe = np.mean(metrics['returns'])/np.std(metrics['returns']) if np.std(metrics['returns']) else 0
    return {
        'tp': tp_mult,
        'sl': sl_mult,
        'sharpe': sharpe,
        'avg_return': np.mean(metrics['returns']),
        'win_rate': np.mean([r > 0 for r in metrics['returns']])
    }

def evaluate_wrapper(args):
    tp, results_subset = args
    return _evaluate_parameters((tp, 0.05, results_subset))

def optimize_strategies(results_df):
    tp_levels = np.linspace(0.05, 0.50, 10)
    results_data = results_df.to_dict('records')
    
    with Pool(cpu_count()) as pool:
        results = list(tqdm(
            pool.imap(evaluate_wrapper, [(tp, results_data) for tp in tp_levels]),
            total=len(tp_levels),
            desc="⚙️ Optimizing Strategies"
        ))
    
    strategy_df = pd.DataFrame([r for r in results if r])
    best_sharpe = strategy_df.loc[strategy_df['sharpe'].idxmax()]
    best_return = strategy_df.loc[strategy_df['avg_return'].idxmax()]
    
    print(f"\n🔍 Best Risk-Adjusted (Sharpe {best_sharpe['sharpe']:.2f}):")
    print(f"   TP: {best_sharpe['tp']*100:.1f}% | SL: 5.0%")
    print(f"\n🚀 Most Profitable (Return {best_return['avg_return']*100:.1f}%):")
    print(f"   TP: {best_return['tp']*100:.1f}% | SL: 5.0%")
    
    return strategy_df

def process_combination(args):
    """Top-level function for parallel processing"""
    tp, sl, results_df, initial_investment = args
    total_profit = 0
    num_trades = len(results_df)
    
    for _, row in results_df.iterrows():
        entry = row['entry_price']
        tp_price = entry * (1 + tp)
        sl_price = entry * (1 - sl)
        
        exit_price = None
        for high, low in zip(row['highs'], row['lows']):
            if high >= tp_price:
                exit_price = tp_price
                break
            if low <= sl_price:
                exit_price = sl_price
                break
        
        if exit_price is None:
            exit_price = row['closes'][-1]
        
        shares = initial_investment / entry
        trade_profit = (exit_price - entry) * shares
        total_profit += trade_profit
    
    total_initial = num_trades * initial_investment
    final_amount = total_initial + total_profit
    return (tp, sl, total_initial, final_amount)

def analyze_performance(results_df, initial_investment=100000):
    tp_levels = np.arange(0.05, 0.55, 0.05)
    sl_levels = np.arange(0.01, 0.11, 0.01)
    valid_combos = [(tp, sl) for tp in tp_levels for sl in sl_levels if tp >= 3*sl]
    
    with Pool(cpu_count()) as pool:
        args = [(tp, sl, results_df, initial_investment) for tp, sl in valid_combos]
        results = list(tqdm(
            pool.imap(process_combination, args),
            total=len(args),
            desc="📊 Analyzing Performance"
        ))
    
    total_initial = sum(ti for _, _, ti, _ in results)
    total_final = sum(fa for _, _, _, fa in results)
    
    print("\n📈 Performance Summary:")
    print(f"Total Initial Investment: ₹{total_initial:,.2f}")
    print(f"Total Final Amount: ₹{total_final:,.2f}")
    print(f"Net Profit: ₹{total_final - total_initial:,.2f}")

    print("\n🏆 Top Performers:")
    for tp, sl, ti, fa in sorted(results, key=lambda x: x[3], reverse=True)[:10]:
        print(f"  TP: {tp*100:.1f}% | SL: {sl*100:.1f}%")
        print(f"  Initial: ₹{ti:,.2f} | Final: ₹{fa:,.2f}")
        print(f"  Return: {(fa/ti-1)*100:.2f}%\n")

if __name__ == '__main__':
    process_breakouts = False
    
    if process_breakouts:
        manager = Manager()
        rate_limiter = RateLimiter(manager.Value('i', 0))
        fetch_token_mapping()
        
        df = pd.read_csv('breakout.csv')
        df['date'] = pd.to_datetime(df['date'])
        results = process_breakouts_with_weekly_filter(df, rate_limiter)
        
        if results:
            pd.DataFrame(results).to_csv('trading_results.csv', index=False)
    else:
        if os.path.exists('trading_results.csv'):
            results_df = pd.read_csv('trading_results.csv')
            for col in ['highs', 'lows', 'closes']:
                results_df[col] = results_df[col].apply(eval)
        else:
            print("❌ Error: trading_results.csv not found")
            exit()

    # Optimize strategies
    strategy_df = optimize_strategies(results_df)
    
    # Performance analysis
    analyze_performance(results_df)
    
    print("\n✅ Analysis completed at", datetime.now().strftime("%Y-%m-%d %H:%M:%S"))
