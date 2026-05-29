const { useEffect, useMemo, useState } = React;

const API = "http://localhost:8080/api/v1";
const BRONZE_LOYALTY = { status: "BRONZE", discount: 5, reservationCount: 0 };

function token() {
  return localStorage.getItem("access_token") || "";
}

function consumeAuthFragment() {
  if (!window.location.hash) return false;
  const params = new URLSearchParams(window.location.hash.slice(1));
  const accessToken = params.get("access_token");
  if (!accessToken) return false;
  localStorage.setItem("access_token", accessToken);
  const idToken = params.get("id_token");
  if (idToken) localStorage.setItem("id_token", idToken);
  window.history.replaceState(null, "", window.location.pathname + window.location.search);
  return true;
}

function startLogin() {
  window.location.href = `${API}/authorize`;
}

async function api(path, options = {}) {
  const headers = { ...(options.headers || {}) };
  if (token()) headers.Authorization = `Bearer ${token()}`;
  if (options.body && !headers["Content-Type"]) headers["Content-Type"] = "application/json";
  const res = await fetch(`${API}${path}`, { ...options, headers });
  if (res.status === 204) return null;
  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) throw new Error(data?.message || res.statusText);
  return data;
}

function formatMoney(value) {
  return new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format(value || 0);
}

function statusLabel(status) {
  const labels = {
    PAID: "Оплачено",
    CANCELED: "Отменено",
    CANCELLED: "Отменено",
    BRONZE: "Бронзовый уровень",
    SILVER: "Серебряный уровень",
    GOLD: "Золотой уровень",
    User: "Пользователь",
    Admin: "Администратор",
  };
  return labels[status] || status || "Неизвестно";
}

function statusClass(status) {
  if (status === "PAID") return "success";
  if (status === "CANCELED" || status === "CANCELLED") return "muted";
  if (status === "GOLD") return "gold";
  if (status === "SILVER") return "silver";
  if (status === "BRONZE") return "bronze";
  return "base";
}

function friendlyError(message) {
  const text = String(message || "").toLowerCase();
  if (text.includes("no rooms available")) return "На выбранные даты нет свободных номеров.";
  if (text.includes("loyalty service")) return "Сервис лояльности временно недоступен. Попробуйте еще раз позже.";
  if (text.includes("payment service")) return "Не удалось провести оплату. Попробуйте еще раз.";
  if (text.includes("reservation service")) return "Сервис бронирований временно недоступен. Попробуйте еще раз позже.";
  if (text.includes("statistics service")) return "Сервис статистики временно недоступен.";
  if (text.includes("unauthorized") || text.includes("401")) return "Сессия истекла. Войдите снова.";
  if (text.includes("invalid enddate")) return "Дата выезда должна быть позже даты заезда.";
  if (text.includes("failed to fetch")) return "Нет связи с сервером. Проверьте, что сервисы запущены.";
  return message || "Что-то пошло не так. Попробуйте еще раз.";
}

function starsLabel(value) {
  const n = Number(value || 0);
  if (n === 1) return "1 звезда";
  if (n >= 2 && n <= 4) return `${n} звезды`;
  return `${n} звезд`;
}

function hotelsLabel(value) {
  const n = Number(value || 0);
  const mod100 = n % 100;
  const mod10 = n % 10;
  if (mod100 >= 11 && mod100 <= 14) return `${n} отелей`;
  if (mod10 === 1) return `${n} отель`;
  if (mod10 >= 2 && mod10 <= 4) return `${n} отеля`;
  return `${n} отелей`;
}

function stayLength(startDate, endDate) {
  const start = new Date(startDate);
  const end = new Date(endDate);
  const days = Math.round((end - start) / (24 * 60 * 60 * 1000));
  return Number.isFinite(days) && days > 0 ? days : 1;
}

function App() {
  const [loggedIn, setLoggedIn] = useState(() => consumeAuthFragment() || Boolean(token()));
  const [tab, setTab] = useState("hotels");
  const [me, setMe] = useState(null);
  const [authError, setAuthError] = useState("");
  const [darkTheme, setDarkTheme] = useState(() => localStorage.getItem("theme") === "dark");

  useEffect(() => {
    document.body.classList.toggle("dark", darkTheme);
    localStorage.setItem("theme", darkTheme ? "dark" : "light");
  }, [darkTheme]);

  useEffect(() => {
    if (!loggedIn) {
      setMe(null);
      return;
    }
    api("/me")
      .then((data) => {
        setMe(data);
        setAuthError("");
      })
      .catch(() => {
        localStorage.removeItem("access_token");
        localStorage.removeItem("id_token");
        setMe(null);
        setLoggedIn(false);
        setAuthError("Сессия истекла. Войдите снова, чтобы бронировать.");
        setTab("hotels");
      });
  }, [loggedIn]);

  const isAdmin = me?.roles?.some((role) => role.toLowerCase() === "admin");
  const title = { hotels: "Отели", reservations: "Бронирования", profile: "Профиль", admin: "Администрирование" }[tab];

  function logout() {
    localStorage.clear();
    setLoggedIn(false);
    setTab("hotels");
  }

  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-mark">HB</span>
          <span>Hotels Booking</span>
        </div>
        <nav className="nav">
          <button className={tab === "hotels" ? "active" : ""} onClick={() => setTab("hotels")}>Отели</button>
          {loggedIn && (
            <>
              <button className={tab === "reservations" ? "active" : ""} onClick={() => setTab("reservations")}>Бронирования</button>
              <button className={tab === "profile" ? "active" : ""} onClick={() => setTab("profile")}>Профиль</button>
            </>
          )}
          {isAdmin && <button className={tab === "admin" ? "active" : ""} onClick={() => setTab("admin")}>Администрирование</button>}
        </nav>
        <div className="sidebar-footer">
          <div className="user-chip">
            <span>{me?.username?.slice(0, 2).toUpperCase() || "GT"}</span>
            <div>
              <strong>{me?.username || "Гость"}</strong>
              <small>{loggedIn ? "Активная сессия" : "Просмотр каталога"}</small>
            </div>
          </div>
          {loggedIn ? (
            <button className="logout" onClick={logout}>Выйти</button>
          ) : (
            <button className="login-button" onClick={startLogin}>Войти</button>
          )}
        </div>
      </aside>
      <main className="content">
        <div className="topbar">
          <div>
            <h1>{title}</h1>
          </div>
          <button className={`theme-toggle ${darkTheme ? "active" : ""}`} type="button" onClick={() => setDarkTheme((value) => !value)} aria-pressed={darkTheme}>
            <span aria-hidden="true"></span>
            {darkTheme ? "Темная" : "Светлая"}
          </button>
        </div>
        {authError && <div className="notice warning">{authError}</div>}
        {tab === "hotels" && <Hotels loggedIn={loggedIn} />}
        {tab === "reservations" && (loggedIn ? <Reservations /> : <AuthRequired />)}
        {tab === "profile" && (loggedIn ? <Profile me={me} reload={() => api("/me").then(setMe)} /> : <AuthRequired />)}
        {tab === "admin" && isAdmin && <Admin />}
      </main>
    </div>
  );
}

function AuthRequired() {
  return (
    <section className="empty-state">
      <h2>Нужен вход</h2>
      <p>Для бронирований и профиля требуется аккаунт.</p>
      <button className="primary" onClick={startLogin}>Войти</button>
    </section>
  );
}

function Hotels({ loggedIn }) {
  const [hotels, setHotels] = useState([]);
  const [totalHotels, setTotalHotels] = useState(null);
  const [booking, setBooking] = useState(null);
  const [error, setError] = useState("");

  useEffect(() => {
    api("/hotels?page=1&size=20")
      .then((data) => {
        setHotels(data.items || []);
        setTotalHotels(data.totalElements ?? data.items?.length ?? 0);
      })
      .catch((err) => setError(friendlyError(err.message)));
  }, []);

  const averagePrice = useMemo(() => {
    if (!hotels.length) return 0;
    return Math.round(hotels.reduce((sum, hotel) => sum + Number(hotel.price || 0), 0) / hotels.length);
  }, [hotels]);

  return (
    <>
      <section className="catalog-summary">
        <div>
          <span className="eyebrow">Каталог</span>
          <h2>{totalHotels == null ? "..." : hotelsLabel(totalHotels)}</h2>
        </div>
        <div className="summary-metrics">
          <Metric label="Средняя цена" value={averagePrice ? formatMoney(averagePrice) : "..."} />
        </div>
      </section>
      {error && <div className="notice warning">{error}</div>}
      <div className="hotel-grid">
        {hotels.map((hotel, index) => (
          <HotelCard key={hotel.hotelUid} hotel={hotel} loggedIn={loggedIn} index={index} onBook={() => loggedIn ? setBooking(hotel) : startLogin()} />
        ))}
      </div>
      {loggedIn && booking && <Booking hotel={booking} close={() => setBooking(null)} />}
    </>
  );
}

function HotelCard({ hotel, loggedIn, index, onBook }) {
  const tone = ["forest", "clay", "ink", "sky"][index % 4];
  const availableRooms = Number(hotel.availableRooms ?? 0);
  const soldOut = availableRooms <= 0;
  return (
    <article className="hotel-card">
      <div className={`hotel-cover ${tone}`}>
        <span>{hotel.city}</span>
        <strong>{starsLabel(hotel.stars || 0)}</strong>
      </div>
      <div className="hotel-body">
        <div>
          <h2>{hotel.name}</h2>
          <p>{hotel.country}, {hotel.city}, {hotel.address}</p>
        </div>
        <div className="hotel-facts">
          <span>{statusLabel("PAID").replace("Оплачено", "Мгновенная оплата")}</span>
          <span>От {formatMoney(hotel.price)} за ночь</span>
        </div>
        <div className="occupancy">
          <span>занято {hotel.occupiedRooms ?? 0} / {hotel.totalRooms ?? 0}</span>
          <strong>свободно {availableRooms}</strong>
        </div>
        <div className="hotel-footer">
          <div>
            <small>Стоимость</small>
            <strong>{formatMoney(hotel.price)}</strong>
          </div>
          <button className="primary" onClick={onBook} disabled={soldOut}>
            {soldOut ? "Нет свободных номеров" : loggedIn ? "Забронировать" : "Войти"}
          </button>
        </div>
      </div>
    </article>
  );
}

function Metric({ label, value }) {
  return (
    <div className="metric">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function Booking({ hotel, close }) {
  const today = new Date().toISOString().slice(0, 10);
  const tomorrow = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString().slice(0, 10);
  const [startDate, setStartDate] = useState(today);
  const [endDate, setEndDate] = useState(tomorrow);
  const [message, setMessage] = useState(null);
  const [busy, setBusy] = useState(false);
  const [loyalty, setLoyalty] = useState(null);
  const [loyaltyUnavailable, setLoyaltyUnavailable] = useState(false);
  const [availability, setAvailability] = useState(hotel);

  const nights = stayLength(startDate, endDate);
  const discount = loyalty?.discount || 0;
  const baseTotal = (hotel.price || 0) * nights;
  const finalTotal = Math.round(baseTotal * (100 - discount) / 100);
  const availableRooms = Number(availability?.availableRooms ?? hotel.availableRooms ?? 0);
  const soldOut = availableRooms <= 0;

  useEffect(() => {
    api("/loyalty")
      .then((data) => {
        setLoyalty(data);
        setLoyaltyUnavailable(false);
      })
      .catch(() => {
        setLoyalty(BRONZE_LOYALTY);
        setLoyaltyUnavailable(true);
      });
  }, []);

  useEffect(() => {
    if (!startDate || !endDate) return;
    const params = new URLSearchParams({ startDate, endDate });
    api(`/hotels/${hotel.hotelUid}?${params.toString()}`)
      .then(setAvailability)
      .catch(() => setAvailability(hotel));
  }, [hotel, startDate, endDate]);

  async function submit(e) {
    e.preventDefault();
    setMessage(null);
    setBusy(true);
    try {
      await api("/reservations", { method: "POST", body: JSON.stringify({ hotelUid: hotel.hotelUid, startDate, endDate }) });
      setMessage({ type: "success", text: "Бронирование создано." });
    } catch (err) {
      setMessage({ type: "error", text: friendlyError(err.message) });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="modal-backdrop" role="presentation">
      <section className="modal">
        <div className="modal-header">
          <div>
            <span className="eyebrow">Новая бронь</span>
            <h2>{hotel.name}</h2>
          </div>
          <button className="icon-button" type="button" onClick={close} aria-label="Закрыть">x</button>
        </div>
        <form className="form" onSubmit={submit}>
          <div className="form-grid">
            <label>Заезд<input type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} required /></label>
            <label>Выезд<input type="date" value={endDate} onChange={(e) => setEndDate(e.target.value)} required /></label>
          </div>
          <div className="booking-total">
            <span>{nights} ноч.</span>
            <div>
              {discount > 0 && <small className="price-before">{formatMoney(baseTotal)}</small>}
              <strong>{formatMoney(finalTotal)}</strong>
            </div>
          </div>
          <div className="booking-notes">
            {loyaltyUnavailable ? (
              <span>Бонусы временно недоступны, применен бронзовый уровень: {discount}%.</span>
            ) : (
              <span>Скидка лояльности: {discount}%</span>
            )}
            <span>Свободно номеров на даты: {availableRooms}</span>
          </div>
          <div className="row">
            <button className="primary" type="submit" disabled={busy || soldOut}>{soldOut ? "Нет свободных номеров" : busy ? "Создаем..." : "Создать бронь"}</button>
            <button className="secondary" type="button" onClick={close}>Закрыть</button>
          </div>
        </form>
        {message && <div className={`notice ${message.type === "error" ? "warning" : "success-note"}`}>{message.text}</div>}
      </section>
    </div>
  );
}

function Reservations() {
  const [items, setItems] = useState([]);
  const [error, setError] = useState("");
  const [confirming, setConfirming] = useState(null);
  const [busy, setBusy] = useState(false);

  const load = () => api("/reservations").then(setItems).catch((err) => setError(friendlyError(err.message)));
  useEffect(() => {
    load();
  }, []);

  async function cancel(uid) {
    setBusy(true);
    try {
      await api(`/reservations/${uid}`, { method: "DELETE" });
      setConfirming(null);
      load();
    } catch (err) {
      setError(friendlyError(err.message));
    } finally {
      setBusy(false);
    }
  }

  if (!items.length && !error) {
    return <section className="empty-state"><h2>Бронирований пока нет</h2><p>Выберите отель в каталоге и оформите первую поездку.</p></section>;
  }

  return (
    <>
      {error && <div className="notice warning">{error}</div>}
      <div className="reservation-list">
        {items.map((reservation) => (
          <article className="reservation-card" key={reservation.reservationUid}>
            <div>
              <h2>{reservation.hotel.name}</h2>
              <p>{reservation.hotel.fullAddress}</p>
              <div className="date-line">{reservation.startDate} - {reservation.endDate}</div>
            </div>
            <div className="reservation-side">
              <span className={`badge ${statusClass(reservation.status)}`}>{statusLabel(reservation.status)}</span>
              {reservation.payment && <strong>{formatMoney(reservation.payment.price)}</strong>}
              {reservation.paymentUnavailable && <span className="badge warning">Оплата временно недоступна</span>}
              {reservation.status !== "CANCELED" && (
                <button className="danger" onClick={() => setConfirming(reservation)}>Отменить</button>
              )}
            </div>
          </article>
        ))}
      </div>
      {confirming && (
        <ConfirmDialog
          title="Отменить бронирование?"
          text={`${confirming.hotel.name}, ${confirming.startDate} - ${confirming.endDate}`}
          confirmText={busy ? "Отменяем..." : "Да, отменить"}
          onCancel={() => setConfirming(null)}
          onConfirm={() => cancel(confirming.reservationUid)}
          disabled={busy}
        />
      )}
    </>
  );
}

function ConfirmDialog({ title, text, confirmText, onCancel, onConfirm, disabled }) {
  return (
    <div className="modal-backdrop" role="presentation">
      <section className="confirm-dialog">
        <h2>{title}</h2>
        <p>{text}</p>
        <div className="row end">
          <button className="secondary" type="button" onClick={onCancel} disabled={disabled}>Оставить</button>
          <button className="danger" type="button" onClick={onConfirm} disabled={disabled}>{confirmText}</button>
        </div>
      </section>
    </div>
  );
}

function Profile({ me }) {
  if (!me) return <div className="notice">Загружаем профиль...</div>;
  const loyalty = me.loyalty || {};
  const loyaltyClass = statusClass(loyalty.status);
  return (
    <div className="profile-grid">
      <section className="profile-card">
        <span className="eyebrow">Аккаунт</span>
        <h2>{me.username}</h2>
        <p>{me.email}</p>
        <div className="badge-row">{me.roles?.map((role) => <span className="badge base" key={role}>{statusLabel(role)}</span>)}</div>
      </section>
      <section className={`profile-card loyalty-card ${loyaltyClass}`}>
        <span className="eyebrow">Лояльность</span>
        <h2>{statusLabel(loyalty.status)}</h2>
        <div className="loyalty-number">{loyalty.discount ?? 0}%</div>
        <p>{loyalty.reservationCount ?? 0} бронирований учтено</p>
      </section>
    </div>
  );
}

function Admin() {
  const [stats, setStats] = useState(null);
  const [statsError, setStatsError] = useState("");
  const [users, setUsers] = useState([]);
  const [form, setForm] = useState({ username: "", email: "", password: "", role: "User" });
  const load = () => {
    api("/statistics")
      .then((data) => {
        setStats(data);
        setStatsError("");
      })
      .catch((err) => {
        setStats(null);
        setStatsError(friendlyError(err.message));
      });
    api("/users").then(setUsers).catch(() => setUsers([]));
  };
  useEffect(() => {
    load();
  }, []);

  async function create(e) {
    e.preventDefault();
    await api("/users", { method: "POST", body: JSON.stringify(form) });
    setForm({ username: "", email: "", password: "", role: "User" });
    load();
  }

  return (
    <>
      <section className="card">
        <h2>Статистика</h2>
        {statsError ? <div className="notice warning">{statsError}</div> : <pre>{JSON.stringify(stats, null, 2)}</pre>}
      </section>
      <section className="card">
        <h2>Пользователи</h2>
        <form className="form" onSubmit={create}>
          <div className="form-grid">
            <label>Логин<input value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} required /></label>
            <label>Email<input value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} required /></label>
            <label>Пароль<input type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} required /></label>
            <label>Роль<select value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value })}><option value="User">Пользователь</option><option value="Admin">Администратор</option></select></label>
          </div>
          <button className="primary" type="submit">Создать</button>
        </form>
      </section>
      <table className="table">
        <thead><tr><th>ID</th><th>Логин</th><th>Email</th><th>Роль</th></tr></thead>
        <tbody>{users.map((u) => <tr key={u.id}><td>{u.id}</td><td>{u.username}</td><td>{u.email}</td><td>{statusLabel(u.role)}</td></tr>)}</tbody>
      </table>
    </>
  );
}

ReactDOM.createRoot(document.getElementById("root")).render(<App />);
